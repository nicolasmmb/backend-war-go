package api

import (
	"strconv"
	"unsafe"
)

type fraudJSONParser struct {
	b []byte
	i int
	n int
}

func parseFraudRequestFastJSON(raw []byte, out *FraudRequest) bool {
	p := fraudJSONParser{b: raw, n: len(raw)}
	*out = FraudRequest{}

	if !p.parseRoot(out) {
		return false
	}
	p.skipWS()
	return p.i == p.n
}

func (p *fraudJSONParser) parseRoot(out *FraudRequest) bool {
	return p.parseObject(func(key []byte) bool {
		switch {
		case keyEqual(key, "id"):
			v, ok := p.parseString()
			if !ok {
				return false
			}
			out.ID = v
			return true
		case keyEqual(key, "transaction"):
			return p.parseTransaction(&out.Transaction)
		case keyEqual(key, "customer"):
			return p.parseCustomer(&out.Customer)
		case keyEqual(key, "merchant"):
			return p.parseMerchant(&out.Merchant)
		case keyEqual(key, "terminal"):
			return p.parseTerminal(&out.Terminal)
		case keyEqual(key, "last_transaction"):
			p.skipWS()
			if p.tryConsume('n') {
				if !p.consumeBytes("ull") {
					return false
				}
				out.LastTx = nil
				return true
			}
			var last LastTxData
			if !p.parseLastTx(&last) {
				return false
			}
			out.LastTx = &last
			return true
		default:
			return p.skipValue()
		}
	})
}

func (p *fraudJSONParser) parseTransaction(out *TransactionData) bool {
	return p.parseObject(func(key []byte) bool {
		switch {
		case keyEqual(key, "amount"):
			v, ok := p.parseFloat32()
			if !ok {
				return false
			}
			out.Amount = v
			return true
		case keyEqual(key, "installments"):
			v, ok := p.parseUint32()
			if !ok {
				return false
			}
			out.Installments = v
			return true
		case keyEqual(key, "requested_at"):
			v, ok := p.parseString()
			if !ok {
				return false
			}
			out.RequestedAt = v
			return true
		default:
			return p.skipValue()
		}
	})
}

func (p *fraudJSONParser) parseCustomer(out *CustomerData) bool {
	return p.parseObject(func(key []byte) bool {
		switch {
		case keyEqual(key, "avg_amount"):
			v, ok := p.parseFloat32()
			if !ok {
				return false
			}
			out.AvgAmount = v
			return true
		case keyEqual(key, "tx_count_24h"):
			v, ok := p.parseUint32()
			if !ok {
				return false
			}
			out.TxCount24h = v
			return true
		case keyEqual(key, "known_merchants"):
			v, ok := p.parseStringArray()
			if !ok {
				return false
			}
			out.KnownMerchants = v
			return true
		default:
			return p.skipValue()
		}
	})
}

func (p *fraudJSONParser) parseMerchant(out *MerchantData) bool {
	return p.parseObject(func(key []byte) bool {
		switch {
		case keyEqual(key, "id"):
			v, ok := p.parseString()
			if !ok {
				return false
			}
			out.ID = v
			return true
		case keyEqual(key, "mcc"):
			v, ok := p.parseString()
			if !ok {
				return false
			}
			out.MCC = v
			return true
		case keyEqual(key, "avg_amount"):
			v, ok := p.parseFloat32()
			if !ok {
				return false
			}
			out.AvgAmount = v
			return true
		default:
			return p.skipValue()
		}
	})
}

func (p *fraudJSONParser) parseTerminal(out *TerminalData) bool {
	return p.parseObject(func(key []byte) bool {
		switch {
		case keyEqual(key, "is_online"):
			v, ok := p.parseBool()
			if !ok {
				return false
			}
			out.IsOnline = v
			return true
		case keyEqual(key, "card_present"):
			v, ok := p.parseBool()
			if !ok {
				return false
			}
			out.CardPresent = v
			return true
		case keyEqual(key, "km_from_home"):
			v, ok := p.parseFloat32()
			if !ok {
				return false
			}
			out.KmFromHome = v
			return true
		default:
			return p.skipValue()
		}
	})
}

func (p *fraudJSONParser) parseLastTx(out *LastTxData) bool {
	return p.parseObject(func(key []byte) bool {
		switch {
		case keyEqual(key, "timestamp"):
			v, ok := p.parseString()
			if !ok {
				return false
			}
			out.Timestamp = v
			return true
		case keyEqual(key, "km_from_current"):
			v, ok := p.parseFloat32()
			if !ok {
				return false
			}
			out.KmFromCurrent = v
			return true
		default:
			return p.skipValue()
		}
	})
}

func (p *fraudJSONParser) parseObject(handle func(key []byte) bool) bool {
	p.skipWS()
	if !p.tryConsume('{') {
		return false
	}
	p.skipWS()
	if p.tryConsume('}') {
		return true
	}
	for {
		key, ok := p.parseKey()
		if !ok {
			return false
		}
		p.skipWS()
		if !p.tryConsume(':') {
			return false
		}
		if !handle(key) {
			return false
		}
		p.skipWS()
		if p.tryConsume('}') {
			return true
		}
		if !p.tryConsume(',') {
			return false
		}
	}
}

func (p *fraudJSONParser) parseKey() ([]byte, bool) {
	p.skipWS()
	if !p.tryConsume('"') {
		return nil, false
	}
	start := p.i
	for p.i < p.n {
		c := p.b[p.i]
		if c == '"' {
			raw := p.b[start:p.i]
			p.i++
			return raw, true
		}
		if c == '\\' || c < 0x20 {
			// Chaves escapadas/invalidas nao fazem parte do payload esperado; invalida decode.
			return nil, false
		}
		p.i++
	}
	return nil, false
}

func (p *fraudJSONParser) parseStringArray() ([]string, bool) {
	p.skipWS()
	if !p.tryConsume('[') {
		return nil, false
	}
	p.skipWS()
	if p.tryConsume(']') {
		return []string{}, true
	}
	out := make([]string, 0, 8)
	for {
		s, ok := p.parseString()
		if !ok {
			return nil, false
		}
		out = append(out, s)
		p.skipWS()
		if p.tryConsume(']') {
			return out, true
		}
		if !p.tryConsume(',') {
			return nil, false
		}
	}
}

func (p *fraudJSONParser) parseString() (string, bool) {
	p.skipWS()
	if !p.tryConsume('"') {
		return "", false
	}
	start := p.i
	escaped := false
	for p.i < p.n {
		c := p.b[p.i]
		if c == '"' {
			raw := p.b[start:p.i]
			p.i++
			if !escaped {
				return string(raw), true
			}
			q := make([]byte, 0, len(raw)+2)
			q = append(q, '"')
			q = append(q, raw...)
			q = append(q, '"')
			u, err := strconv.Unquote(string(q))
			if err != nil {
				return "", false
			}
			return u, true
		}
		if c == '\\' {
			escaped = true
			p.i += 2
			continue
		}
		if c < 0x20 {
			return "", false
		}
		p.i++
	}
	return "", false
}

func (p *fraudJSONParser) parseFloat32() (float32, bool) {
	p.skipWS()
	start := p.i
	if p.i < p.n && (p.b[p.i] == '-' || p.b[p.i] == '+') {
		p.i++
	}
	hasDigit := false
	for p.i < p.n {
		c := p.b[p.i]
		if c >= '0' && c <= '9' {
			hasDigit = true
			p.i++
			continue
		}
		break
	}
	if p.i < p.n && p.b[p.i] == '.' {
		p.i++
		for p.i < p.n {
			c := p.b[p.i]
			if c >= '0' && c <= '9' {
				hasDigit = true
				p.i++
				continue
			}
			break
		}
	}
	if !hasDigit {
		return 0, false
	}
	if p.i < p.n && (p.b[p.i] == 'e' || p.b[p.i] == 'E') {
		p.i++
		if p.i < p.n && (p.b[p.i] == '-' || p.b[p.i] == '+') {
			p.i++
		}
		expDigit := false
		for p.i < p.n {
			c := p.b[p.i]
			if c >= '0' && c <= '9' {
				expDigit = true
				p.i++
				continue
			}
			break
		}
		if !expDigit {
			return 0, false
		}
	}
	v, err := strconv.ParseFloat(bytesToString(p.b[start:p.i]), 32)
	if err != nil {
		return 0, false
	}
	return float32(v), true
}

func (p *fraudJSONParser) parseUint32() (uint32, bool) {
	p.skipWS()
	if p.i >= p.n {
		return 0, false
	}
	if p.b[p.i] == '+' || p.b[p.i] == '-' {
		return 0, false
	}
	var v uint64
	digits := 0
	for p.i < p.n {
		c := p.b[p.i]
		if c >= '0' && c <= '9' {
			v = v*10 + uint64(c-'0')
			if v > uint64(^uint32(0)) {
				return 0, false
			}
			digits++
			p.i++
			continue
		}
		break
	}
	if digits == 0 {
		return 0, false
	}
	if p.i < p.n {
		if p.b[p.i] == '.' || p.b[p.i] == 'e' || p.b[p.i] == 'E' {
			return 0, false
		}
	}
	return uint32(v), true
}

func (p *fraudJSONParser) parseBool() (bool, bool) {
	p.skipWS()
	if p.tryConsume('t') {
		if !p.consumeBytes("rue") {
			return false, false
		}
		return true, true
	}
	if p.tryConsume('f') {
		if !p.consumeBytes("alse") {
			return false, false
		}
		return false, true
	}
	return false, false
}

func (p *fraudJSONParser) skipValue() bool {
	p.skipWS()
	if p.i >= p.n {
		return false
	}
	switch p.b[p.i] {
	case '{':
		p.i++
		p.skipWS()
		if p.tryConsume('}') {
			return true
		}
		for {
			if _, ok := p.parseString(); !ok {
				return false
			}
			p.skipWS()
			if !p.tryConsume(':') {
				return false
			}
			if !p.skipValue() {
				return false
			}
			p.skipWS()
			if p.tryConsume('}') {
				return true
			}
			if !p.tryConsume(',') {
				return false
			}
		}
	case '[':
		p.i++
		p.skipWS()
		if p.tryConsume(']') {
			return true
		}
		for {
			if !p.skipValue() {
				return false
			}
			p.skipWS()
			if p.tryConsume(']') {
				return true
			}
			if !p.tryConsume(',') {
				return false
			}
		}
	case '"':
		_, ok := p.parseString()
		return ok
	case 't', 'f':
		_, ok := p.parseBool()
		return ok
	case 'n':
		p.i++
		return p.consumeBytes("ull")
	default:
		_, ok := p.parseFloat32()
		return ok
	}
}

func (p *fraudJSONParser) skipWS() {
	for p.i < p.n {
		switch p.b[p.i] {
		case ' ', '\n', '\r', '\t':
			p.i++
		default:
			return
		}
	}
}

func (p *fraudJSONParser) tryConsume(c byte) bool {
	if p.i < p.n && p.b[p.i] == c {
		p.i++
		return true
	}
	return false
}

func (p *fraudJSONParser) consumeBytes(s string) bool {
	if p.i+len(s) > p.n {
		return false
	}
	for j := range len(s) {
		if p.b[p.i+j] != s[j] {
			return false
		}
	}
	p.i += len(s)
	return true
}

func keyEqual(key []byte, lit string) bool {
	if len(key) != len(lit) {
		return false
	}
	for i := 0; i < len(lit); i++ {
		if key[i] != lit[i] {
			return false
		}
	}
	return true
}

func bytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}
