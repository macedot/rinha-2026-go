package httpresp

// Pre-computed response bodies.
// ScoreBody indexed by fraud count (0-5) for zero-overhead response.
var (
	ScoreBody     [6][]byte
	Ready         = []byte{}
	NotFound      = []byte{}
	BadRequest    = []byte(`{"error":"invalid_payload"}`)
	InternalError = []byte{}
)

func Init() {
	ScoreBody[0] = []byte(`{"approved":true,"fraud_score":0.0}`)
	ScoreBody[1] = []byte(`{"approved":true,"fraud_score":0.2}`)
	ScoreBody[2] = []byte(`{"approved":true,"fraud_score":0.4}`)
	ScoreBody[3] = []byte(`{"approved":false,"fraud_score":0.6}`)
	ScoreBody[4] = []byte(`{"approved":false,"fraud_score":0.8}`)
	ScoreBody[5] = []byte(`{"approved":false,"fraud_score":1.0}`)
}
