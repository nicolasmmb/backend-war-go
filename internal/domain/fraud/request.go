package fraud

import (
	"errors"
	"strings"
)

var (
	ErrMissingTransactionTimestamp = errors.New("transaction.requested_at is required")
	ErrMissingLastTxTimestamp      = errors.New("last_transaction.timestamp is required")
)

// Request representa o comando de analise antifraude no dominio.
type Request struct {
	ID          string
	Transaction Transaction
	Customer    Customer
	Merchant    Merchant
	Terminal    Terminal
	LastTx      *LastTransaction
}

// Transaction agrega os dados do evento de compra.
type Transaction struct {
	Amount       float32
	Installments uint32
	RequestedAt  string
}

// Customer agrega o historico relevante para a decisao.
type Customer struct {
	AvgAmount      float32
	TxCount24h     uint32
	KnownMerchants []string
}

// Merchant representa o estabelecimento da transacao.
type Merchant struct {
	ID        string
	MCC       string
	AvgAmount float32
}

// Terminal representa o contexto do ponto de captura.
type Terminal struct {
	IsOnline    bool
	CardPresent bool
	KmFromHome  float32
}

// LastTransaction representa a ultima compra conhecida do cliente.
type LastTransaction struct {
	Timestamp     string
	KmFromCurrent float32
}

// Validate aplica regras minimas de consistencia do comando de fraude.
func (r Request) Validate() error {
	if strings.TrimSpace(r.Transaction.RequestedAt) == "" {
		return ErrMissingTransactionTimestamp
	}
	if r.LastTx != nil && strings.TrimSpace(r.LastTx.Timestamp) == "" {
		return ErrMissingLastTxTimestamp
	}
	return nil
}
