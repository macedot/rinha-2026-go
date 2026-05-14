package vectorizer

// Build extracts a 14-dim feature vector from the JSON body.
// Single-pass positional parser — reads fields in order like the C impl.
// No key searching, no allocations.
func Build(body []byte) ([14]float32, bool) {
	var q [14]float32
	pos := 0

	// { "id": "...", ... }
	pos = skipWS(body, pos)
	if body[pos] != '{' {
		return q, false
	}
	pos++
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ',' {
		return q, false
	}
	pos++

	// "transaction": { amount, installments, requested_at }
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	pos = skipWS(body, pos)
	if body[pos] != '{' {
		return q, false
	}
	pos++

	// "amount": <number>
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	amount, newPos := parseF32(body, pos)
	pos = newPos
	pos = skipWS(body, pos)
	if body[pos] != ',' {
		return q, false
	}
	pos++

	// "installments": <number>
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	installments, newPos := parseF32(body, pos)
	pos = newPos
	pos = skipWS(body, pos)
	if body[pos] != ',' {
		return q, false
	}
	pos++

	// "requested_at": <string>
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	pos = skipWS(body, pos)
	var reqTsBuf [40]byte
	reqTs, newPos := parseString(body, pos, reqTsBuf[:])
	pos = newPos
	pos = skipWS(body, pos)
	if body[pos] != '}' {
		return q, false
	}
	pos++
	pos = skipWS(body, pos)
	if body[pos] != ',' {
		return q, false
	}
	pos++

	// "customer": { avg_amount, tx_count_24h, known_merchants }
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	pos = skipWS(body, pos)
	if body[pos] != '{' {
		return q, false
	}
	pos++

	// "avg_amount": <number>
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	customerAvgAmount, newPos := parseF32(body, pos)
	pos = newPos
	pos = skipWS(body, pos)
	if body[pos] != ',' {
		return q, false
	}
	pos++

	// "tx_count_24h": <number>
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	txCount24h, newPos := parseF32(body, pos)
	pos = newPos
	pos = skipWS(body, pos)
	if body[pos] != ',' {
		return q, false
	}
	pos++

	// "known_merchants": [ ... ]
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	pos = skipWS(body, pos)
	if body[pos] != '[' {
		return q, false
	}
	pos++

	// Parse known_merchants array
	type merchantRef struct {
		data []byte
	}
	var merchants [32]merchantRef
	numMerchants := 0
	for {
		pos = skipWS(body, pos)
		if body[pos] == ']' {
			pos++
			break
		}
		if body[pos] == '"' {
			pos++
			start := pos
			for pos < len(body) && body[pos] != '"' {
				pos++
			}
			if numMerchants < 32 {
				merchants[numMerchants].data = body[start:pos]
				numMerchants++
			}
			pos++ // skip closing "
		}
		pos = skipWS(body, pos)
		if pos < len(body) && body[pos] == ',' {
			pos++
		}
	}
	pos = skipWS(body, pos)
	if body[pos] != '}' {
		return q, false
	}
	pos++
	pos = skipWS(body, pos)
	if body[pos] != ',' {
		return q, false
	}
	pos++

	// "merchant": { id, mcc, avg_amount }
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	pos = skipWS(body, pos)
	if body[pos] != '{' {
		return q, false
	}
	pos++

	// "id": <string>
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	pos = skipWS(body, pos)
	var merchantIDBuf [64]byte
	merchantID, newPos := parseString(body, pos, merchantIDBuf[:])
	pos = newPos
	pos = skipWS(body, pos)
	if body[pos] != ',' {
		return q, false
	}
	pos++

	// "mcc": <string>
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	pos = skipWS(body, pos)
	var mccBuf [16]byte
	mcc, newPos := parseString(body, pos, mccBuf[:])
	pos = newPos
	pos = skipWS(body, pos)
	if body[pos] != ',' {
		return q, false
	}
	pos++

	// "avg_amount": <number>
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	merchantAvgAmount, newPos := parseF32(body, pos)
	pos = newPos
	pos = skipWS(body, pos)
	if body[pos] != '}' {
		return q, false
	}
	pos++
	pos = skipWS(body, pos)
	if body[pos] != ',' {
		return q, false
	}
	pos++

	// "terminal": { is_online, card_present, km_from_home }
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	pos = skipWS(body, pos)
	if body[pos] != '{' {
		return q, false
	}
	pos++

	// "is_online": <bool>
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	isOnline := parseBool(body, pos)
	if isOnline {
		pos += 4 // true
	} else {
		pos += 5 // false
	}

	pos = skipWS(body, pos)
	if body[pos] != ',' {
		return q, false
	}
	pos++

	// "card_present": <bool>
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	cardPresent := parseBool(body, pos)
	if cardPresent {
		pos += 4
	} else {
		pos += 5
	}

	pos = skipWS(body, pos)
	if body[pos] != ',' {
		return q, false
	}
	pos++

	// "km_from_home": <number>
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	kmFromHome, newPos := parseF32(body, pos)
	pos = newPos
	pos = skipWS(body, pos)
	if body[pos] != '}' {
		return q, false
	}
	pos++
	pos = skipWS(body, pos)
	if body[pos] != ',' {
		return q, false
	}
	pos++

	// "last_transaction": null or { timestamp, km_from_current }
	pos = skipString(body, pos)
	pos = skipWS(body, pos)
	if body[pos] != ':' {
		return q, false
	}
	pos++
	pos = skipWS(body, pos)

	minutesSinceLastTx := float32(-1.0)
	kmFromLastTx := float32(-1.0)

	if body[pos] == 'n' {
		pos += 4 // null
	} else {
		// { timestamp, km_from_current }
		if body[pos] != '{' {
			return q, false
		}
		pos++

		pos = skipString(body, pos)
		pos = skipWS(body, pos)
		if body[pos] != ':' {
			return q, false
		}
		pos++
		pos = skipWS(body, pos)
		var lastTsBuf [40]byte
		lastTs, newPos := parseString(body, pos, lastTsBuf[:])
		pos = newPos
		pos = skipWS(body, pos)
		if body[pos] != ',' {
			return q, false
		}
		pos++

		pos = skipString(body, pos)
		pos = skipWS(body, pos)
		if body[pos] != ':' {
			return q, false
		}
		pos++
		kmFromCurrent, newPos := parseF32(body, pos)
		pos = newPos
		pos = skipWS(body, pos)
		if body[pos] != '}' {
			return q, false
		}
		pos++

		mins := minutesBetweenAbs(reqTs, lastTs)
		minutesSinceLastTx = clamp01(float32(mins) / 1440.0)
		_ = kmFromCurrent
		kmFromLastTx = clamp01(kmFromCurrent / 1000.0)
	}

	// Check if merchant_id is in known_merchants
	knownMerchant := false
	for i := 0; i < numMerchants; i++ {
		if len(merchants[i].data) == len(merchantID) {
			match := true
			for j := 0; j < len(merchantID); j++ {
				if merchants[i].data[j] != merchantID[j] {
					match = false
					break
				}
			}
			if match {
				knownMerchant = true
				break
			}
		}
	}

	unknownMerchant := float32(1.0)
	if knownMerchant {
		unknownMerchant = 0.0
	}

	ratio := float32(1.0)
	if customerAvgAmount > 0 {
		ratio = clamp01((amount / customerAvgAmount) / 10.0)
	}

	// Parse MCC code
	mccCode := 0
	for i := 0; i < len(mcc) && i < 4; i++ {
		if mcc[i] >= '0' && mcc[i] <= '9' {
			mccCode = mccCode*10 + int(mcc[i]-'0')
		}
	}

	q[0] = clamp01(amount / 10000.0)
	q[1] = clamp01(installments / 12.0)
	q[2] = ratio
	q[3] = clamp01(float32(isoHourUTC(reqTs)) / 23.0)
	q[4] = clamp01(float32(weekdayFromISO(reqTs)) / 6.0)
	q[5] = minutesSinceLastTx
	q[6] = kmFromLastTx
	q[7] = clamp01(kmFromHome / 1000.0)
	q[8] = clamp01(txCount24h / 20.0)
	if isOnline {
		q[9] = 1.0
	} else {
		q[9] = 0.0
	}
	if cardPresent {
		q[10] = 1.0
	} else {
		q[10] = 0.0
	}
	q[11] = unknownMerchant
	q[12] = mccRisk(mccCode)
	q[13] = clamp01(merchantAvgAmount / 10000.0)

	return q, true
}

// --- Zero-alloc positional parsing primitives ---

func skipWS(body []byte, pos int) int {
	for pos < len(body) && (body[pos] == ' ' || body[pos] == '\n' || body[pos] == '\r' || body[pos] == '\t') {
		pos++
	}
	return pos
}

// skipString skips a key name (quoted string) and positions after closing quote
func skipString(body []byte, pos int) int {
	pos = skipWS(body, pos)
	if pos >= len(body) || body[pos] != '"' {
		return pos
	}
	pos++ // skip opening "
	for pos < len(body) {
		if body[pos] == '\\' {
			pos += 2
			continue
		}
		if body[pos] == '"' {
			return pos + 1
		}
		pos++
	}
	return pos
}

// parseString reads a quoted string value into buf, returns slice and new pos
func parseString(body []byte, pos int, buf []byte) ([]byte, int) {
	pos = skipWS(body, pos)
	if pos >= len(body) || body[pos] != '"' {
		return nil, pos
	}
	pos++ // skip opening "
	start := pos
	for pos < len(body) {
		if body[pos] == '\\' {
			pos += 2
			continue
		}
		if body[pos] == '"' {
			n := copy(buf, body[start:pos])
			return buf[:n], pos + 1
		}
		pos++
	}
	return nil, pos
}

// parseF32 reads a float number, returns value and new position
func parseF32(body []byte, pos int) (float32, int) {
	pos = skipWS(body, pos)
	if pos >= len(body) {
		return 0, pos
	}
	start := pos
	neg := false
	if body[pos] == '-' {
		neg = true
		pos++
	}
	var integer float32
	for pos < len(body) && body[pos] >= '0' && body[pos] <= '9' {
		integer = integer*10 + float32(body[pos]-'0')
		pos++
	}
	var frac float32
	if pos < len(body) && body[pos] == '.' {
		pos++
		div := float32(10.0)
		for pos < len(body) && body[pos] >= '0' && body[pos] <= '9' {
			frac += float32(body[pos]-'0') / div
			div *= 10
			pos++
		}
	}
	// skip exponent if present (e.g. 1.5e+10)
	if pos < len(body) && (body[pos] == 'e' || body[pos] == 'E') {
		pos++
		if pos < len(body) && (body[pos] == '+' || body[pos] == '-') {
			pos++
		}
		for pos < len(body) && body[pos] >= '0' && body[pos] <= '9' {
			pos++
		}
	}
	if pos == start {
		return 0, pos
	}
	result := integer + frac
	if neg {
		result = -result
	}
	return result, pos
}

func parseBool(body []byte, pos int) bool {
	pos = skipWS(body, pos)
	if pos+3 < len(body) && body[pos] == 't' {
		return true
	}
	return false
}

// --- Utility ---

func clamp01(x float32) float32 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func mccRisk(code int) float32 {
	switch code {
	case 5411:
		return 0.15
	case 5812:
		return 0.30
	case 5912:
		return 0.20
	case 5944:
		return 0.45
	case 7801:
		return 0.80
	case 7802:
		return 0.75
	case 7995:
		return 0.85
	case 4511:
		return 0.35
	case 5311:
		return 0.25
	case 5999:
		return 0.50
	default:
		return 0.50
	}
}

// --- Date/time helpers ---

func isoHourUTC(s []byte) int {
	if len(s) < 13 {
		return 0
	}
	h := int(s[11]-'0')*10 + int(s[12]-'0')
	if h > 23 {
		return 23
	}
	return h
}

func weekdayFromISO(s []byte) int {
	if len(s) < 10 {
		return 0
	}
	y := int(s[0]-'0')*1000 + int(s[1]-'0')*100 + int(s[2]-'0')*10 + int(s[3]-'0')
	m := int(s[5]-'0')*10 + int(s[6]-'0')
	d := int(s[8]-'0')*10 + int(s[9]-'0')

	if m <= 2 {
		y--
	}
	days := int64(365*y+y/4-y/100+y/400+(153*(m-3+12*((m-3)>>31))+2)/5+d-719469)
	w := int((days+3)%7 + 7) % 7
	return w
}

func minutesBetweenAbs(a, b []byte) int64 {
	aMins := isoToMinutes(a)
	bMins := isoToMinutes(b)
	diff := aMins - bMins
	if diff < 0 {
		diff = -diff
	}
	return diff
}

func isoToMinutes(s []byte) int64 {
	if len(s) < 16 {
		return 0
	}
	y := int(s[0]-'0')*1000 + int(s[1]-'0')*100 + int(s[2]-'0')*10 + int(s[3]-'0')
	m := int(s[5]-'0')*10 + int(s[6]-'0')
	d := int(s[8]-'0')*10 + int(s[9]-'0')
	h := int(s[11]-'0')*10 + int(s[12]-'0')
	mi := int(s[14]-'0')*10 + int(s[15]-'0')
	if m <= 2 {
		y--
	}
	days := int64(365*y+y/4-y/100+y/400+(153*(m-3+12*((m-3)>>31))+2)/5+d-719469)
	return days*1440 + int64(h)*60 + int64(mi)
}

var _ = float32(0)
