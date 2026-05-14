package vectorizer

// Build extracts a 14-dim feature vector from the JSON body.
// Uses hardcoded divisors matching the C implementation.
func Build(body []byte) ([14]float32, bool) {
	var q [14]float32

	transaction, ok := objectRange(body, "transaction")
	if !ok {
		return q, false
	}
	customer, ok := objectRange(body, "customer")
	if !ok {
		return q, false
	}
	merchant, ok := objectRange(body, "merchant")
	if !ok {
		return q, false
	}
	terminal, ok := objectRange(body, "terminal")
	if !ok {
		return q, false
	}

	amount, ok1 := jsonNumber(transaction, "amount")
	installments, ok2 := jsonNumber(transaction, "installments")
	var reqTsBuf [40]byte
	requestedAt, ok3 := jsonString(transaction, "requested_at", reqTsBuf[:])
	if !ok1 || !ok2 || !ok3 {
		return q, false
	}

	customerAvgAmount, ok4 := jsonNumber(customer, "avg_amount")
	txCount24h, ok5 := jsonNumber(customer, "tx_count_24h")
	if !ok4 || !ok5 {
		return q, false
	}

	var merchantIDBuf [64]byte
	merchantID, ok6 := jsonString(merchant, "id", merchantIDBuf[:])
	var mccBuf [16]byte
	mcc, ok7 := jsonString(merchant, "mcc", mccBuf[:])
	merchantAvgAmount, ok8 := jsonNumber(merchant, "avg_amount")
	if !ok6 || !ok7 || !ok8 {
		return q, false
	}

	isOnline, ok9 := jsonBool(terminal, "is_online")
	cardPresent, ok10 := jsonBool(terminal, "card_present")
	kmFromHome, ok11 := jsonNumber(terminal, "km_from_home")
	if !ok9 || !ok10 || !ok11 {
		return q, false
	}

	minutesSinceLastTx := float32(-1.0)
	kmFromLastTx := float32(-1.0)

	ltObj, ok := objectRange(body, "last_transaction")
	if ok {
		var lastTsBuf [40]byte
		lastTs, ok1 := jsonString(ltObj, "timestamp", lastTsBuf[:])
		kmFromCurrent, ok2 := jsonNumber(ltObj, "km_from_current")
		if ok1 && ok2 {
			mins := minutesBetweenAbs(requestedAt, lastTs)
			minutesSinceLastTx = clamp01(float32(mins) / 1440.0)
			kmFromLastTx = clamp01(kmFromCurrent / 1000.0)
		}
	}

	knownMerchant := arrayContainsString(customer, "known_merchants", merchantID)
	unknownMerchant := float32(1.0)
	if knownMerchant {
		unknownMerchant = 0.0
	}

	amountVsAvg := float32(1.0)
	if customerAvgAmount > 0 {
		amountVsAvg = clamp01((amount / customerAvgAmount) / 10.0)
	}

	// Parse MCC code for risk lookup
	mccCode := 0
	for i := 0; i < len(mcc) && i < 4; i++ {
		if mcc[i] >= '0' && mcc[i] <= '9' {
			mccCode = mccCode*10 + int(mcc[i]-'0')
		}
	}

	q[0] = clamp01(amount / 10000.0)
	q[1] = clamp01(installments / 12.0)
	q[2] = amountVsAvg
	q[3] = clamp01(float32(isoHourUTC(requestedAt)) / 23.0)
	q[4] = clamp01(float32(weekdayFromISO(requestedAt)) / 6.0)
	q[5] = minutesSinceLastTx
	q[6] = kmFromLastTx
	q[7] = clamp01(kmFromHome / 1000.0)
	q[8] = clamp01(txCount24h / 20.0)
	if isOnline == 1 {
		q[9] = 1.0
	} else {
		q[9] = 0.0
	}
	if cardPresent == 1 {
		q[10] = 1.0
	} else {
		q[10] = 0.0
	}
	q[11] = unknownMerchant
	q[12] = mccRisk(mccCode)
	q[13] = clamp01(merchantAvgAmount / 10000.0)

	return q, true
}

func clamp01(x float32) float32 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// mccRisk returns the fraud risk for a given MCC code.
// Hardcoded defaults matching the C implementation.
var mccRiskTable = map[int]float32{
	5411: 0.15, 5812: 0.30, 5912: 0.20,
	5944: 0.45, 7801: 0.80, 7802: 0.75,
	7995: 0.85, 4511: 0.35, 5311: 0.25,
	5999: 0.50,
}

func mccRisk(code int) float32 {
	if v, ok := mccRiskTable[code]; ok {
		return v
	}
	return 0.50
}

// --- JSON parsing helpers (zero-alloc) ---

func skipWS(p []byte) []byte {
	i := 0
	for i < len(p) && (p[i] == ' ' || p[i] == '\n' || p[i] == '\r' || p[i] == '\t') {
		i++
	}
	return p[i:]
}

func findChar(p []byte, ch byte) int {
	for i := range p {
		if p[i] == ch {
			return i
		}
	}
	return -1
}

// findKeyRange finds the value range for a top-level key in a JSON object.
// Returns the value bytes (after colon) and true if found.
func findKeyRange(data []byte, key string) ([]byte, bool) {
	p := data
	if len(p) > 0 && p[0] == '{' {
		p = p[1:]
	}

	for len(p) > 0 {
		p = skipWS(p)
		if len(p) == 0 || p[0] != '"' {
			return nil, false
		}
		p = p[1:]
		keyEnd := findChar(p, '"')
		if keyEnd == -1 {
			return nil, false
		}
		foundKey := string(p[:keyEnd])
		p = p[keyEnd+1:]
		p = skipWS(p)
		if len(p) == 0 || p[0] != ':' {
			return nil, false
		}
		p = p[1:]
		p = skipWS(p)

		if foundKey == key {
			return p, true
		}

		// Skip this value
		p = skipValue(p)
		if len(p) > 0 && p[0] == ',' {
			p = p[1:]
		}
	}
	return nil, false
}

func skipValue(p []byte) []byte {
	if len(p) == 0 {
		return p
	}
	switch p[0] {
	case '"':
		p = p[1:]
		for len(p) > 0 {
			if p[0] == '\\' {
				p = p[2:]
				continue
			}
			if p[0] == '"' {
				return p[1:]
			}
			p = p[1:]
		}
		return p
	case '{':
		depth := 1
		p = p[1:]
		for len(p) > 0 && depth > 0 {
			switch p[0] {
			case '{':
				depth++
			case '}':
				depth--
			case '"':
				p = p[1:]
				for len(p) > 0 {
					if p[0] == '\\' {
						p = p[2:]
						continue
					}
					if p[0] == '"' {
						break
					}
					p = p[1:]
				}
			}
			p = p[1:]
		}
		return p
	case '[':
		depth := 1
		p = p[1:]
		for len(p) > 0 && depth > 0 {
			switch p[0] {
			case '[':
				depth++
			case ']':
				depth--
			case '"':
				p = p[1:]
				for len(p) > 0 {
					if p[0] == '\\' {
						p = p[2:]
						continue
					}
					if p[0] == '"' {
						break
					}
					p = p[1:]
				}
			}
			p = p[1:]
		}
		return p
	default:
		// number, bool, null
		for len(p) > 0 && p[0] != ',' && p[0] != '}' && p[0] != ']' {
			p = p[1:]
		}
		return p
	}
}

func objectRange(data []byte, key string) ([]byte, bool) {
	val, ok := findKeyRange(data, key)
	if !ok || len(val) == 0 {
		return nil, false
	}
	if val[0] != '{' {
		return nil, false
	}
	// Find matching closing brace
	depth := 0
	for i := 0; i < len(val); i++ {
		if val[i] == '{' {
			depth++
		} else if val[i] == '}' {
			depth--
			if depth == 0 {
				return val[:i+1], true
			}
		} else if val[i] == '"' {
			i++
			for i < len(val) {
				if val[i] == '\\' {
					i++
					continue
				}
				if val[i] == '"' {
					break
				}
				i++
			}
		}
	}
	return nil, false
}

func jsonNumber(data []byte, key string) (float32, bool) {
	val, ok := findKeyRange(data, key)
	if !ok {
		return 0, false
	}
	return parseNumber(val)
}

func parseNumber(p []byte) (float32, bool) {
	p = skipWS(p)
	if len(p) == 0 {
		return 0, false
	}

	neg := false
	if p[0] == '-' {
		neg = true
		p = p[1:]
	}

	var integer float32
	hasDigit := false
	for len(p) > 0 && p[0] >= '0' && p[0] <= '9' {
		integer = integer*10 + float32(p[0]-'0')
		p = p[1:]
		hasDigit = true
	}

	var frac float32
	if len(p) > 0 && p[0] == '.' {
		p = p[1:]
		div := float32(10.0)
		for len(p) > 0 && p[0] >= '0' && p[0] <= '9' {
			frac += float32(p[0]-'0') / div
			div *= 10
			p = p[1:]
			hasDigit = true
		}
	}

	if !hasDigit {
		return 0, false
	}

	result := integer + frac
	if neg {
		result = -result
	}
	return result, true
}

func jsonString(data []byte, key string, buf []byte) ([]byte, bool) {
	val, ok := findKeyRange(data, key)
	if !ok {
		return nil, false
	}
	val = skipWS(val)
	if len(val) < 2 || val[0] != '"' {
		return nil, false
	}
	val = val[1:]
	end := findChar(val, '"')
	if end == -1 {
		return nil, false
	}
	n := copy(buf, val[:end])
	return buf[:n], true
}

func jsonBool(data []byte, key string) (int, bool) {
	val, ok := findKeyRange(data, key)
	if !ok {
		return 0, false
	}
	val = skipWS(val)
	if len(val) >= 4 && string(val[:4]) == "true" {
		return 1, true
	}
	if len(val) >= 5 && string(val[:5]) == "false" {
		return 0, true
	}
	return 0, false
}

func arrayContainsString(data []byte, key string, needle []byte) bool {
	val, ok := findKeyRange(data, key)
	if !ok {
		return false
	}
	val = skipWS(val)
	if len(val) == 0 || val[0] != '[' {
		return false
	}
	val = val[1:]

	for len(val) > 0 {
		val = skipWS(val)
		if len(val) == 0 || val[0] == ']' {
			break
		}
		if val[0] == '"' {
			val = val[1:]
			end := findChar(val, '"')
			if end == -1 {
				break
			}
			if end == len(needle) {
				match := true
				for i := 0; i < end; i++ {
					if val[i] != needle[i] {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
			val = val[end+1:]
		} else {
			val = skipValue(val)
		}
		if len(val) > 0 && val[0] == ',' {
			val = val[1:]
		}
	}
	return false
}

// --- Date/time helpers ---

func isoYear(s []byte) int {
	return int(s[0]-'0')*1000 + int(s[1]-'0')*100 + int(s[2]-'0')*10 + int(s[3]-'0')
}
func isoMonth(s []byte) int  { return int(s[5]-'0')*10 + int(s[6]-'0') }
func isoDay(s []byte) int    { return int(s[8]-'0')*10 + int(s[9]-'0') }
func isoHour(s []byte) int   { return int(s[11]-'0')*10 + int(s[12]-'0') }
func isoMinute(s []byte) int { return int(s[14]-'0')*10 + int(s[15]-'0') }
func isoSecond(s []byte) int { return int(s[17]-'0')*10 + int(s[18]-'0') }

func isoHourUTC(s []byte) int {
	if len(s) < 19 {
		return 0
	}
	return isoHour(s)
}

// isoToEpochSeconds converts ISO timestamp to epoch seconds (UTC)
func isoToEpochSeconds(s []byte) int64 {
	y, m, d := isoYear(s), isoMonth(s), isoDay(s)
	h, mi, sec := isoHour(s), isoMinute(s), isoSecond(s)

	// Days from epoch using Tomohiko Sakamoto's algorithm
	if m <= 2 {
		y--
	}
	days := int64(365*y + y/4 - y/100 + y/400 + (153*(m-3+12*((m-3)>>31))+2)/5 + d - 719469)
	return days*86400 + int64(h)*3600 + int64(mi)*60 + int64(sec)
}

// weekdayFromISO returns 0=Mon..6=Sun
func weekdayFromISO(s []byte) int {
	if len(s) < 10 {
		return 0
	}
	sec := isoToEpochSeconds(s)
	// 1970-01-01 was Thursday (3 in Mon=0..Sun=6)
	w := int((sec/86400 + 3) % 7)
	if w < 0 {
		w += 7
	}
	return w
}

func minutesBetweenAbs(a, b []byte) int64 {
	diff := isoToEpochSeconds(a) - isoToEpochSeconds(b)
	if diff < 0 {
		diff = -diff
	}
	return diff / 60
}
