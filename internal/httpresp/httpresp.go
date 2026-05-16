package httpresp

var (
	ScoreBody     [6][]byte
	ScoreLen      [6]int
	Ready         []byte
	ReadyLen      int
	NotFound      []byte
	NotFoundLen   int
	BadRequest    []byte
	BadRequestLen int
	InternalError []byte
	InternalErrLen int
)

func Init() {
	ScoreBody[0] = []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 35\r\n\r\n{\"approved\":true,\"fraud_score\":0.0}")
	ScoreBody[1] = []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 35\r\n\r\n{\"approved\":true,\"fraud_score\":0.2}")
	ScoreBody[2] = []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 35\r\n\r\n{\"approved\":true,\"fraud_score\":0.4}")
	ScoreBody[3] = []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 36\r\n\r\n{\"approved\":false,\"fraud_score\":0.6}")
	ScoreBody[4] = []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 36\r\n\r\n{\"approved\":false,\"fraud_score\":0.8}")
	ScoreBody[5] = []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 36\r\n\r\n{\"approved\":false,\"fraud_score\":1.0}")
	for i := range ScoreLen {
		ScoreLen[i] = len(ScoreBody[i])
	}

	Ready = []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK")
	ReadyLen = len(Ready)

	NotFound = []byte("HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n")
	NotFoundLen = len(NotFound)

	BadRequest = []byte("HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	BadRequestLen = len(BadRequest)

	InternalError = []byte("HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	InternalErrLen = len(InternalError)
}
