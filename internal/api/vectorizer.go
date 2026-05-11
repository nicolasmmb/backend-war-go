package api

import (
	"backend/internal/ivf"
	"fmt"
)

const (
	// Limites de normalizacao mantem cada feature em escala comparavel para distancias L2.
	maxAmount            = float32(10_000.0)
	maxInstallments      = float32(12.0)
	amountVsAvgRatio     = float32(10.0)
	maxMinutes           = float32(1440.0)
	maxKM                = float32(1000.0)
	maxTxCount24h        = float32(20.0)
	maxMerchantAvgAmount = float32(10_000.0)

	invMaxAmount            = float32(1.0 / 10_000.0)
	invMaxInstallments      = float32(1.0 / 12.0)
	invAmountVsAvgRatio     = float32(1.0 / 10.0)
	invMaxMinutes           = float32(1.0 / 1440.0)
	invMaxKM                = float32(1.0 / 1000.0)
	invMaxTxCount24h        = float32(1.0 / 20.0)
	invMaxMerchantAvgAmount = float32(1.0 / 10_000.0)
	invWeekdayMax           = float32(1.0 / 6.0)
	invHourMax              = float32(1.0 / 23.0)

	missingLastTxFeature = float32(-1.0)

	isoDateTimeMinLen = 20
	isoYearSepPos     = 4
	isoMonthSepPos    = 7
	isoDateTimeSepPos = 10
	isoHourSepPos     = 13
	isoMinuteSepPos   = 16

	civilFebMonth = 2

	civilEraYears          = 400
	civilNegativeEraAdjust = 399
	civilMarchOffset       = 3
	civilJanFebOffset      = 9
	civilDoyMul            = 153
	civilDoyBias           = 2
	civilDoyDiv            = 5
	civilEpochOffsetDays   = 719468
	daysPerCivilEra        = 146097
	daysPerYear            = 365
	daysPerQuarterCentury  = 100
	daysPerWeek            = 7

	secondsPerMinute = 60
	secondsPerHour   = 60 * secondsPerMinute
	secondsPerDay    = 24 * secondsPerHour

	weekdayMondayOffset = 3
)

// Vectorize transforma o payload HTTP nas 14 features esperadas pelo indice vetorial.
func Vectorize(req *FraudRequest) ([ivf.Dim]float32, error) {
	// Passo 0: inicializa vetor de saida zerado para retorno seguro em caso de erro.
	var out [ivf.Dim]float32

	// parseia timestamp principal da transacao;
	y, m, d, hour, min, sec, ok := parseISODateTime(req.Transaction.RequestedAt)
	if !ok {
		return out, fmt.Errorf("invalid requested_at")
	}

	// deriva dia da semana (segunda=0..domingo=6)
	weekday := weekdayMon0(y, m, d)

	// inicializa features dependentes da ultima transacao com sentinela de "ausente".
	minsLast := missingLastTxFeature
	kmLast := missingLastTxFeature
	if req.LastTx != nil {
		// valida timestamp da ultima transacao.
		ly, lm, ld, lh, lmin, lsec, lok := parseISODateTime(req.LastTx.Timestamp)
		if !lok {
			return out, fmt.Errorf("invalid last_transaction.timestamp")
		}
		// calcula distancia temporal/espacial entre transacoes e normaliza para [0,1].
		mins := minutesBetweenAbs(y, m, d, hour, min, sec, ly, lm, ld, lh, lmin, lsec)
		minsLast = clamp(float32(mins) * invMaxMinutes)
		kmLast = clamp(req.LastTx.KmFromCurrent * invMaxKM)
	}

	// computa feature de "valor atual vs historico"; fallback para 1.0 quando media e invalida/zero.
	amountVsAvg := float32(1.0)
	if req.Customer.AvgAmount > 0 {
		amountVsAvg = clamp((req.Transaction.Amount / req.Customer.AvgAmount) * invAmountVsAvgRatio)
	}

	// monta as 14 features na ordem exata aprendida no treinamento do indice.
	// Ordem das features precisa permanecer identica ao dataset de treinamento.
	out = [ivf.Dim]float32{
		clamp(req.Transaction.Amount * invMaxAmount),
		clamp(float32(req.Transaction.Installments) * invMaxInstallments),
		amountVsAvg,
		float32(hour) * invHourMax,
		float32(weekday) * invWeekdayMax,
		// Valor negativo sinaliza ausencia de ultima transacao sem colidir com dominio normalizado [0,1].
		minsLast,
		kmLast,
		clamp(req.Terminal.KmFromHome * invMaxKM),
		clamp(float32(req.Customer.TxCount24h) * invMaxTxCount24h),
		boolToFloat(req.Terminal.IsOnline),
		boolToFloat(req.Terminal.CardPresent),
		boolToFloat(!merchantIDInKnown(req)),
		mccRisk(req.Merchant.MCC),
		clamp(req.Merchant.AvgAmount * invMaxMerchantAvgAmount),
	}
	return out, nil
}

// clamp normaliza valores para o intervalo [0,1] usado no vetor.
func clamp(x float32) float32 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// boolToFloat converte flag booleana para feature numerica binaria.
func boolToFloat(v bool) float32 {
	if v {
		return 1
	}
	return 0
}

// mccRisk aplica risco heuristico fixo por MCC, com fallback neutro para codigos desconhecidos.
func mccRisk(mcc string) float32 {
	switch mcc {
	case "5411":
		return 0.15
	case "5812":
		return 0.30
	case "5912":
		return 0.20
	case "5944":
		return 0.45
	case "7801":
		return 0.80
	case "7802":
		return 0.75
	case "7995":
		return 0.85
	case "4511":
		return 0.35
	case "5311":
		return 0.25
	case "5999":
		return 0.50
	default:
		return 0.5
	}
}

// parseISODateTime faz parsing posicional de timestamp ISO8601 para evitar custo de time.Parse no hot path.
func parseISODateTime(s string) (year, month, day, hour, min, sec int, ok bool) {
	// Precisamos apenas da porcao "YYYY-MM-DDTHH:MM:SS"; sufixos (ex.: Z) podem existir.
	if len(s) < isoDateTimeMinLen {
		return 0, 0, 0, 0, 0, 0, false
	}
	if s[isoYearSepPos] != '-' || s[isoMonthSepPos] != '-' || s[isoDateTimeSepPos] != 'T' || s[isoHourSepPos] != ':' || s[isoMinuteSepPos] != ':' {
		return 0, 0, 0, 0, 0, 0, false
	}
	year, ok = parse4Digits(s[0], s[1], s[2], s[3])
	if !ok {
		return
	}
	month, ok = parse2Digits(s[5], s[6])
	if !ok {
		return
	}
	day, ok = parse2Digits(s[8], s[9])
	if !ok {
		return
	}
	hour, ok = parse2Digits(s[11], s[12])
	if !ok {
		return
	}
	min, ok = parse2Digits(s[14], s[15])
	if !ok {
		return
	}
	sec, ok = parse2Digits(s[17], s[18])
	if !ok {
		return
	}
	if month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || min > 59 || sec > 59 {
		return 0, 0, 0, 0, 0, 0, false
	}
	return year, month, day, hour, min, sec, true
}

// parse2Digits converte dois bytes ASCII numericos para inteiro.
func parse2Digits(a, b byte) (int, bool) {
	if a < '0' || a > '9' || b < '0' || b > '9' {
		return 0, false
	}
	return int(a-'0')*10 + int(b-'0'), true
}

// parse4Digits converte quatro bytes ASCII numericos para inteiro.
func parse4Digits(a, b, c, d byte) (int, bool) {
	v1, ok := parse2Digits(a, b)
	if !ok {
		return 0, false
	}
	v2, ok := parse2Digits(c, d)
	if !ok {
		return 0, false
	}
	return v1*100 + v2, true
}

// daysFromCivil converte data civil em dias absolutos desde epoca fixa para calculos de calendario.
func daysFromCivil(y, m, d int) int64 {
	y2 := y
	if m <= civilFebMonth {
		y2--
	}
	era := 0
	if y2 >= 0 {
		era = y2 / civilEraYears
	} else {
		era = (y2 - civilNegativeEraAdjust) / civilEraYears
	}
	yoe := y2 - era*civilEraYears
	var mp int
	if m > civilFebMonth {
		mp = m - civilMarchOffset
	} else {
		mp = m + civilJanFebOffset
	}
	doy := (civilDoyMul*mp+civilDoyBias)/civilDoyDiv + d - 1
	doe := yoe*daysPerYear + yoe/4 - yoe/daysPerQuarterCentury + doy
	return int64(era*daysPerCivilEra + doe - civilEpochOffsetDays)
}

// weekdayMon0 retorna dia da semana no formato segunda=0 ... domingo=6.
func weekdayMon0(y, m, d int) int {
	days := daysFromCivil(y, m, d)
	w := (days + weekdayMondayOffset) % daysPerWeek
	if w < 0 {
		w += daysPerWeek
	}
	return int(w)
}

// epochSeconds converte data/hora UTC em segundos para calcular diferenca temporal entre transacoes.
func epochSeconds(y, m, d, h, min, sec int) int64 {
	return daysFromCivil(y, m, d)*secondsPerDay + int64(h*secondsPerHour+min*secondsPerMinute+sec)
}

// minutesBetweenAbs retorna diferenca absoluta em minutos entre dois timestamps.
func minutesBetweenAbs(y1, m1, d1, h1, min1, sec1, y2, m2, d2, h2, min2, sec2 int) int64 {
	if y1 == y2 && m1 == m2 && d1 == d2 {
		a := int64(h1*secondsPerHour + min1*secondsPerMinute + sec1)
		b := int64(h2*secondsPerHour + min2*secondsPerMinute + sec2)
		if a > b {
			return (a - b) / secondsPerMinute
		}
		return (b - a) / secondsPerMinute
	}
	a := epochSeconds(y1, m1, d1, h1, min1, sec1)
	b := epochSeconds(y2, m2, d2, h2, min2, sec2)
	if a > b {
		return (a - b) / secondsPerMinute
	}
	return (b - a) / secondsPerMinute
}
