package vectorizer

// Build extracts a 14-dim feature vector from the JSON body.
// Uses a strict sequential single-pass parser matching the C implementation.
func Build(body []byte) ([14]float32, bool) {
	var q [14]float32
	p := &parser{s: body}

	// { "id": "...", ... }
	expectChar(p, '{')
	skipString(p)
	expectChar(p, ':')
	skipString(p)
	expectChar(p, ',')

	// "transaction": { amount, installments, requested_at }
	skipString(p)
	expectChar(p, ':')
	expectChar(p, '{')
	skipString(p)
	expectChar(p, ':')
	amount := parseF32(p)
	expectChar(p, ',')
	skipString(p)
	expectChar(p, ':')
	installments := parseF32(p)
	expectChar(p, ',')
	skipString(p)
	expectChar(p, ':')
	reqTs := parseStringBounds(p)
	expectChar(p, '}')
	expectChar(p, ',')

	// "customer": { avg_amount, tx_count_24h, known_merchants }
	skipString(p)
	expectChar(p, ':')
	expectChar(p, '{')
	skipString(p)
	expectChar(p, ':')
	customerAvgAmount := parseF32(p)
	expectChar(p, ',')
	skipString(p)
	expectChar(p, ':')
	txCount24h := parseF32(p)
	expectChar(p, ',')
	skipString(p)
	expectChar(p, ':')

	var merchants [32][]byte
	numMerchants := 0
	expectChar(p, '[')
	for p.pos < len(p.s) && p.s[p.pos] != ']' {
		prev := p.pos
		m := parseStringBounds(p)
		if p.pos == prev {
			break // malformed
		}
		if numMerchants < 32 {
			merchants[numMerchants] = m
			numMerchants++
		}
		skipWs(p)
		if p.pos < len(p.s) && p.s[p.pos] == ',' {
			p.pos++
		}
	}
	if p.pos < len(p.s) && p.s[p.pos] == ']' {
		p.pos++
	}
	expectChar(p, '}')
	expectChar(p, ',')

	// "merchant": { id, mcc, avg_amount }
	skipString(p)
	expectChar(p, ':')
	expectChar(p, '{')
	skipString(p)
	expectChar(p, ':')
	merchantID := parseStringBounds(p)
	expectChar(p, ',')
	skipString(p)
	expectChar(p, ':')
	mccStr := parseStringBounds(p)
	expectChar(p, ',')
	skipString(p)
	expectChar(p, ':')
	merchantAvgAmount := parseF32(p)
	expectChar(p, '}')
	expectChar(p, ',')

	knownMerchant := 0
	for i := 0; i < numMerchants; i++ {
		if len(merchants[i]) == len(merchantID) && string(merchants[i]) == string(merchantID) {
			knownMerchant = 1
			break
		}
	}

	// "terminal": { is_online, card_present, km_from_home }
	skipString(p)
	expectChar(p, ':')
	expectChar(p, '{')
	skipString(p)
	expectChar(p, ':')
	isOnline := parseBool(p)
	expectChar(p, ',')
	skipString(p)
	expectChar(p, ':')
	cardPresent := parseBool(p)
	expectChar(p, ',')
	skipString(p)
	expectChar(p, ':')
	kmFromHome := parseF32(p)
	expectChar(p, '}')
	expectChar(p, ',')

	// "last_transaction": null or { timestamp, km_from_current }
	skipString(p)
	expectChar(p, ':')
	minutesSinceLastTx := float32(-1.0)
	kmFromCurrent := float32(-1.0)
	skipWs(p)
	if p.pos < len(p.s) && p.s[p.pos] == 'n' {
		p.pos += 4
	} else {
		expectChar(p, '{')
		skipString(p)
		expectChar(p, ':')
		lastTs := parseStringBounds(p)
		expectChar(p, ',')
		skipString(p)
		expectChar(p, ':')
		kmFromCurrent = parseF32(p)
		expectChar(p, '}')

		reqMins := isoToEpochMinutes(reqTs)
		lastMins := isoToEpochMinutes(lastTs)
		diff := reqMins - lastMins
		if diff < 0 {
			diff = -diff
		}
		minutesSinceLastTx = clamp01(float32(diff) / 1440.0)
		kmFromCurrent = clamp01(kmFromCurrent / 1000.0)
	}

	mccCode := 0
	for i := 0; i < len(mccStr) && i < 4; i++ {
		if mccStr[i] >= '0' && mccStr[i] <= '9' {
			mccCode = mccCode*10 + int(mccStr[i]-'0')
		}
	}

	amountVsAvg := float32(1.0)
	if customerAvgAmount > 0 {
		amountVsAvg = clamp01((amount / customerAvgAmount) / 10.0)
	}

	q[0] = clamp01(amount / 10000.0)
	q[1] = clamp01(installments / 12.0)
	q[2] = amountVsAvg
	q[3] = clamp01(float32(isoHourUTC(reqTs)) / 23.0)
	q[4] = clamp01(float32(weekdayFromISO(reqTs)) / 6.0)
	q[5] = minutesSinceLastTx
	q[6] = kmFromCurrent
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
	if knownMerchant == 1 {
		q[11] = 0.0
	} else {
		q[11] = 1.0
	}
	q[12] = mccRisk(mccCode)
	q[13] = clamp01(merchantAvgAmount / 10000.0)

	// Since we blindly parsed, check if we parsed successfully without going out of bounds
	if p.pos > len(p.s) {
		return q, false
	}

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

type parser struct {
	s   []byte
	pos int
}

func skipWs(p *parser) {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\n' || p.s[p.pos] == '\r' || p.s[p.pos] == '\t') {
		p.pos++
	}
}

func expectChar(p *parser, c byte) {
	skipWs(p)
	if p.pos < len(p.s) && p.s[p.pos] == c {
		p.pos++
	}
}

func skipString(p *parser) {
	skipWs(p)
	if p.pos < len(p.s) && p.s[p.pos] == '"' {
		p.pos++
		for p.pos < len(p.s) && p.s[p.pos] != '"' {
			p.pos++
		}
		if p.pos < len(p.s) {
			p.pos++
		}
	}
}

func parseStringBounds(p *parser) []byte {
	skipWs(p)
	if p.pos < len(p.s) && p.s[p.pos] == '"' {
		p.pos++
		start := p.pos
		for p.pos < len(p.s) && p.s[p.pos] != '"' {
			p.pos++
		}
		end := p.pos
		if p.pos < len(p.s) {
			p.pos++
		}
		return p.s[start:end]
	}
	return nil
}

func parseF32(p *parser) float32 {
	skipWs(p)
	neg := false
	if p.pos < len(p.s) && p.s[p.pos] == '-' {
		neg = true
		p.pos++
	}
	var intPart uint
	for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
		intPart = intPart*10 + uint(p.s[p.pos]-'0')
		p.pos++
	}
	v := float32(intPart)
	if p.pos < len(p.s) && p.s[p.pos] == '.' {
		p.pos++
		var frac uint
		fracDigits := 0
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' && fracDigits < 6 {
			frac = frac*10 + uint(p.s[p.pos]-'0')
			fracDigits++
			p.pos++
		}
		if fracDigits > 0 {
			pow10neg := []float32{1.0, 0.1, 0.01, 0.001, 0.0001, 0.00001, 0.000001}
			v += float32(frac) * pow10neg[fracDigits]
		}
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
	}
	if neg {
		return -v
	}
	return v
}

func parseBool(p *parser) int {
	skipWs(p)
	if p.pos+4 <= len(p.s) && string(p.s[p.pos:p.pos+4]) == "true" {
		p.pos += 4
		return 1
	}
	if p.pos+5 <= len(p.s) && string(p.s[p.pos:p.pos+5]) == "false" {
		p.pos += 5
		return 0
	}
	return 0
}

func isoYear(s []byte) int {
	if len(s) < 4 { return 0 }
	return int(s[0]-'0')*1000 + int(s[1]-'0')*100 + int(s[2]-'0')*10 + int(s[3]-'0')
}
func isoMonth(s []byte) int {
	if len(s) < 7 { return 0 }
	return int(s[5]-'0')*10 + int(s[6]-'0')
}
func isoDay(s []byte) int {
	if len(s) < 10 { return 0 }
	return int(s[8]-'0')*10 + int(s[9]-'0')
}
func isoHour(s []byte) int {
	if len(s) < 13 { return 0 }
	return int(s[11]-'0')*10 + int(s[12]-'0')
}
func isoMinute(s []byte) int {
	if len(s) < 16 { return 0 }
	return int(s[14]-'0')*10 + int(s[15]-'0')
}

func isoHourUTC(s []byte) int {
	if len(s) < 19 {
		return 0
	}
	return isoHour(s)
}

func isoToEpochMinutes(s []byte) int64 {
	y, m, d := isoYear(s), isoMonth(s), isoDay(s)
	h, mi := isoHour(s), isoMinute(s)

	if m <= 2 {
		y--
	}
	days := int64(365*y + y/4 - y/100 + y/400 + (153*(m-3+12*((m-3)>>31))+2)/5 + d - 719469)
	return days*1440 + int64(h)*60 + int64(mi)
}

func weekdayFromISO(s []byte) int {
	if len(s) < 10 {
		return 0
	}
	mins := isoToEpochMinutes(s)
	days := mins / 1440
	w := int((days + 3) % 7)
	if w < 0 {
		w += 7
	}
	return w
}
