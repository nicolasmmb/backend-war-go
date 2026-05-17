package main

// Config concentra knobs operacionais para separar deploy/runtime da logica de negocio.
type Config struct {
	PORT int `default:"9999"` // PORT define a porta TCP usada quando UDS_PATH nao estiver configurado.

	UDS_PATH string `default:""` // UDS_PATH habilita listen em Unix Domain Socket (ex.: /sockets/api1.sock).

	UDS_MODE uint32 `default:"438"` // UDS_MODE define permissao do arquivo de socket em decimal (438 = 0666).

	USE_MMAP bool `default:"true"` // USE_MMAP controla se o index.bin sera carregado com mmap (true) ou leitura convencional (false).

	INDEX_PATH string `default:"resources/index.bin"` // INDEX_PATH aponta para o arquivo RIVF (index.bin) usado na busca vetorial.

	NPROBE int `default:"8"` // NPROBE define quantos centroides iniciais sao sondados antes da etapa completa de refinamento.
}
