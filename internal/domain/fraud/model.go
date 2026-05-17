package fraud

const (
	// FeatureVectorSize define o tamanho fixo das features usadas no indice vetorial.
	FeatureVectorSize = 14
	// MaxFraudCount representa o total de vizinhos considerados na decisao (top-5).
	MaxFraudCount = uint8(5)
	// MinFraudCountForRejection define o limiar de bloqueio.
	MinFraudCountForRejection = uint8(3)
)

// FeatureVector representa o vetor de features usado na classificacao.
type FeatureVector [FeatureVectorSize]float32

// Assessment concentra o resultado de negocio da analise antifraude.
type Assessment struct {
	FraudCount uint8
	Approved   bool
	FraudScore float32
}

// NewAssessment cria uma decisao consistente a partir da contagem de fraude dos vizinhos.
func NewAssessment(fraudCount uint8) Assessment {
	if fraudCount > MaxFraudCount {
		fraudCount = MaxFraudCount
	}
	return Assessment{
		FraudCount: fraudCount,
		Approved:   fraudCount < MinFraudCountForRejection,
		FraudScore: float32(fraudCount) / float32(MaxFraudCount),
	}
}
