package api

// FraudRequest representa exatamente o contrato HTTP definido pela rinha.
type FraudRequest struct {
	ID          string          `json:"id"`
	Transaction TransactionData `json:"transaction"`
	Customer    CustomerData    `json:"customer"`
	Merchant    MerchantData    `json:"merchant"`
	Terminal    TerminalData    `json:"terminal"`
	LastTx      *LastTxData     `json:"last_transaction"`
}

// TransactionData agrega os campos variaveis por tentativa de compra.
type TransactionData struct {
	Amount       float32 `json:"amount"`
	Installments uint32  `json:"installments"`
	RequestedAt  string  `json:"requested_at"`
}

// CustomerData concentra sinais historicos do cliente usados na vetorizacao.
type CustomerData struct {
	AvgAmount      float32  `json:"avg_amount"`
	TxCount24h     uint32   `json:"tx_count_24h"`
	KnownMerchants []string `json:"known_merchants"`
}

// MerchantData representa metadados do lojista no momento da autorizacao.
type MerchantData struct {
	ID        string  `json:"id"`
	MCC       string  `json:"mcc"`
	AvgAmount float32 `json:"avg_amount"`
}

// TerminalData captura contexto fisico/online do dispositivo de pagamento.
type TerminalData struct {
	IsOnline    bool    `json:"is_online"`
	CardPresent bool    `json:"card_present"`
	KmFromHome  float32 `json:"km_from_home"`
}

// LastTxData traz o ultimo evento conhecido para extrair features temporais/espaciais.
type LastTxData struct {
	Timestamp     string  `json:"timestamp"`
	KmFromCurrent float32 `json:"km_from_current"`
}

// merchantIDInKnown marca se o merchant atual ja foi visto pelo cliente; isso vira feature de risco.
func merchantIDInKnown(req *FraudRequest) bool {
	return merchantIDInKnownLinear(req.Customer.KnownMerchants, req.Merchant.ID)
}

// Loop manual evita custo generico de helpers de slices no caminho quente de vetorizacao.
func merchantIDInKnownLinear(known []string, merchantID string) bool {
	for _, id := range known {
		if id == merchantID {
			return true
		}
	}
	return false
}
