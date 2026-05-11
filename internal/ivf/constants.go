package ivf

const (
	// Dim define largura fixa do vetor aprendida no dataset oficial.
	Dim = 14
	// Magic e Version permitem rejeitar arquivo invalido/incompativel no startup.
	Magic   = "IVFX"
	Version = uint32(3)
	// HeaderSize inclui magic, versao, n, k, dim, escala e bytes reservados.
	HeaderSize = 32
	// BlockSize/Stride definem bloco SoA compacto para varredura vetorizada.
	BlockSize   = 8
	BlockStride = Dim * BlockSize
	// FixScale converte [-1,1] float para dominio int16 sem perder granularidade util.
	FixScale = float32(10_000.0)
)

// PadValue preenche lanes vazias no ultimo bloco de cada cluster.
const PadValue = int16(0x7fff)
