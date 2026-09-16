package atc

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/skycontrol/skycontrol/internal/airfield"
	"github.com/skycontrol/skycontrol/internal/weather"
)

var nato = map[rune]string{
	'A': "alpha", 'B': "bravo", 'C': "charlie", 'D': "delta",
	'E': "echo", 'F': "foxtrot", 'G': "golf", 'H': "hotel",
	'I': "india", 'J': "juliet", 'K': "kilo", 'L': "lima",
	'M': "mike", 'N': "november", 'O': "oscar", 'P': "papa",
	'Q': "quebec", 'R': "romeo", 'S': "sierra", 'T': "tango",
	'U': "uniform", 'V': "victor", 'W': "whiskey", 'X': "xray",
	'Y': "yankee", 'Z': "zulu",
}

var digits = map[rune]string{
	'0': "zero", '1': "one", '2': "two", '3': "three", '4': "four",
	'5': "five", '6': "six", '7': "seven", '8': "eight", '9': "niner",
}

// spokenNames: display spelling → Piper spelling (one word, no pause).
var spokenNames = []struct{ from, to string }{
	{"Nellis", "Nelliss"},
	{"Creech", "Creech"},
	{"Incirlik", "Innsirlick"},
	{"Al Dhafra", "Al Daffruh"},
	{"Al Maktoum", "Al Macktoom"},
	{"Bagram", "Bagrum"},
	{"Kandahar", "Kanduhhar"},
	{"Ramstein", "Ramstyne"},
	{"Aviano", "Aveeahno"},
	{"Senaki", "Senahkee"},
	{"Kutaisi", "Kootysee"},
	{"Tbilisi", "Tuhbeeleesee"},
	{"Batumi", "Bahtoomee"},
	{"Vaziani", "Vahzeeahnee"},
	{"Beslan", "Bezlahn"},
	{"Mozdok", "Mozdock"},
}

func SpeakRunway(name string) string {
	name = strings.TrimSpace(strings.ToUpper(name))
	name = strings.TrimPrefix(name, "RWY")
	name = strings.TrimPrefix(name, "RUNWAY")
	name = strings.TrimSpace(name)
	if name == "" {
		return "runway"
	}
	var parts []string
	for _, r := range name {
		switch {
		case unicode.IsDigit(r):
			if w, ok := digits[r]; ok {
				parts = append(parts, w)
			}
		case unicode.IsLetter(r):
			if w, ok := nato[r]; ok {
				parts = append(parts, w)
			}
		}
	}
	if len(parts) == 0 {
		return "runway " + strings.ToLower(name)
	}
	// Hyphens = less robotic pause: two-one-lima
	return "runway " + strings.Join(parts, "-")
}

// ReciprocalRunway: 09 → 27, 21L → 03R. Same strip, opposite end.
func ReciprocalRunway(name string) string {
	name = strings.ToUpper(strings.TrimSpace(name))
	name = strings.TrimPrefix(name, "RWY")
	name = strings.TrimPrefix(name, "RUNWAY")
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	side := ""
	if n := len(name); n > 0 {
		switch name[n-1] {
		case 'L':
			side = "R"
			name = name[:n-1]
		case 'R':
			side = "L"
			name = name[:n-1]
		case 'C':
			side = "C"
			name = name[:n-1]
		}
	}
	num, err := strconv.Atoi(name)
	if err != nil || num <= 0 {
		return ""
	}
	num = (num + 18) % 36
	if num == 0 {
		num = 36
	}
	return fmt.Sprintf("%02d%s", num, side)
}

func pickActiveRunway(af *airfield.Airfield, heading float64, w weather.Sample) string {
	ends := runwayEnds(af)
	if len(ends) == 0 {
		return ""
	}
	// DCS 6-knot rule: calm → first listed end (our file lists the DCS default first).
	if w.OK && w.SpeedKt < weather.SwitchKt {
		return ends[0].Name
	}
	target := heading
	if w.OK && w.SpeedKt >= weather.SwitchKt {
		// Land / take off INTO the wind: runway heading ≈ wind FROM.
		target = w.FromDeg
	}
	best := ends[0]
	bestD := headingDelta(target, runwayHeading(best))
	for _, r := range ends[1:] {
		d := headingDelta(target, runwayHeading(r))
		if d < bestD {
			best, bestD = r, d
		}
	}
	return best.Name
}

func runwayEnds(af *airfield.Airfield) []airfield.Runway {
	if af == nil {
		return nil
	}
	var out []airfield.Runway
	for _, r := range af.Runways {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			continue
		}
		if strings.Contains(name, "/") {
			for _, part := range strings.Split(name, "/") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				rr := r
				rr.Name = part
				rr.HeadingMag = headingFromName(part)
				out = append(out, rr)
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

func runwayHeading(r airfield.Runway) float64 {
	if r.HeadingMag != 0 {
		return r.HeadingMag
	}
	return headingFromName(r.Name)
}

func headingFromName(name string) float64 {
	name = strings.ToUpper(strings.TrimSpace(name))
	name = strings.TrimRight(name, "LRC")
	n, err := strconv.Atoi(name)
	if err != nil {
		return 0
	}
	return float64(n * 10)
}

// rewriteHoldShort stops "taxi to 09, hold short 27" (same strip).
func rewriteHoldShort(text, active string) string {
	if text == "" || active == "" {
		return text
	}
	rec := ReciprocalRunway(active)
	if rec == "" || rec == active {
		return text
	}
	actSpoken := strings.TrimPrefix(SpeakRunway(active), "runway ")
	recSpoken := strings.TrimPrefix(SpeakRunway(rec), "runway ")
	low := strings.ToLower(text)
	recSpaced := strings.ReplaceAll(recSpoken, "-", " ")
	if !strings.Contains(low, recSpoken) && !strings.Contains(low, recSpaced) {
		return text
	}
	repl := func(s, old, neu string) string {
		return replaceCI(s, old, neu)
	}
	text = repl(text, "runway "+recSpoken, "runway "+actSpoken)
	text = repl(text, "runway "+recSpaced, "runway "+actSpoken)
	text = repl(text, recSpoken, actSpoken)
	text = repl(text, recSpaced, actSpoken)
	return text
}

func replaceCI(s, old, neu string) string {
	if old == "" {
		return s
	}
	low := strings.ToLower(s)
	old = strings.ToLower(old)
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(low[i:], old)
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		j += i
		b.WriteString(s[i:j])
		b.WriteString(neu)
		i = j + len(old)
	}
}

// SpeakCallsign keeps the radio name. SkyEye-style "Name | Genius 1-1" → Genius 1-1.
func SpeakCallsign(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.LastIndex(raw, "|"); i >= 0 {
		if right := strings.TrimSpace(raw[i+1:]); right != "" {
			return right
		}
	}
	return raw
}

func PronounceATC(text string) string {
	out := text
	for _, n := range spokenNames {
		out = strings.ReplaceAll(out, n.from, n.to)
	}
	out = strings.ReplaceAll(out, "via alpha", "veeah taxi way alpha")
	out = strings.ReplaceAll(out, "via Alpha", "veeah taxi way alpha")
	out = regexp.MustCompile(`(?i)\bCOM\s*2\b`).ReplaceAllString(out, "comm two")
	out = regexp.MustCompile(`(?i)\bCOM2\b`).ReplaceAllString(out, "comm two")
	out = regexp.MustCompile(`(?i)\bC\.O\.M\.?\s*2\b`).ReplaceAllString(out, "comm two")
	out = regexp.MustCompile(`(?i)\bzero\b`).ReplaceAllString(out, "zee-ro")
	out = dashBetweenDigits.ReplaceAllString(out, "$1 $2")
	out = strings.ReplaceAll(out, "x-ray", "xray")
	out = strings.ReplaceAll(out, "X-ray", "xray")
	out = strings.ReplaceAll(out, "X-Ray", "xray")
	out = regexp.MustCompile(`(?i)\bx\s+ray\b`).ReplaceAllString(out, "xray")
	out = tacanChan.ReplaceAllStringFunc(out, speakTACANMatch)
	out = tacanBandX.ReplaceAllString(out, "$1 xray")
	out = tacanBandY.ReplaceAllString(out, "$1 yankee")
	return out
}

func speakTACANMatch(s string) string {
	return SpeakTACAN(s)
}

func SpeakFrequency(raw string) string {
	s := strings.TrimSpace(strings.ToUpper(raw))
	s = strings.TrimSuffix(s, "AM")
	s = strings.TrimSuffix(s, "FM")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	mhz, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return strings.ToLower(raw)
	}
	str := fmt.Sprintf("%.3f", mhz)
	str = strings.TrimRight(str, "0")
	str = strings.TrimRight(str, ".")
	parts := strings.SplitN(str, ".", 2)
	var b strings.Builder
	for i, r := range parts[0] {
		if i > 0 {
			b.WriteByte(' ')
		}
		if w, ok := digits[r]; ok {
			b.WriteString(w)
		}
	}
	if len(parts) == 2 && parts[1] != "" {
		b.WriteString(" decimal ")
		for i, r := range parts[1] {
			if i > 0 {
				b.WriteByte(' ')
			}
			if w, ok := digits[r]; ok {
				b.WriteString(w)
			}
		}
	}
	return b.String()
}

func SpeakTACAN(raw string) string {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	var parts []string
	for _, r := range raw {
		switch {
		case unicode.IsDigit(r):
			if w, ok := digits[r]; ok {
				parts = append(parts, w)
			}
		case r == 'X':
			parts = append(parts, "xray")
		case r == 'Y':
			parts = append(parts, "yankee")
		}
	}
	if len(parts) == 0 {
		return strings.ToLower(raw)
	}
	return strings.Join(parts, " ")
}

func SpeakForRadio(text string) string {
	out := PronounceATC(text)
	out = strings.ReplaceAll(out, "You're", "you are")
	out = strings.ReplaceAll(out, "you're", "you are")
	var b strings.Builder
	for _, r := range out {
		if w, ok := digits[r]; ok {
			b.WriteByte(' ')
			b.WriteString(w)
			b.WriteByte(' ')
			continue
		}
		switch r {
		case '-', ',', '.', ';', ':', '!', '?', '\'', '"', '(', ')', '…':
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

var dashBetweenDigits = regexp.MustCompile(`(\d)-(\d)`)
var tacanChan = regexp.MustCompile(`(?i)\b(\d{1,3})\s*([XY])\b`)
var tacanBandX = regexp.MustCompile(`(?i)((?:zero|one|two|three|four|five|six|seven|eight|niner|nine)(?:\s+(?:zero|one|two|three|four|five|six|seven|eight|niner|nine)){0,2})\s+[Xx]\b`)
var tacanBandY = regexp.MustCompile(`(?i)((?:zero|one|two|three|four|five|six|seven|eight|niner|nine)(?:\s+(?:zero|one|two|three|four|five|six|seven|eight|niner|nine)){0,2})\s+[Yy]\b`)

// SpokenTaxi is the Piper-tested taxi line.
// Display text should stay FAA order; this is the mouth only.
func SpokenTaxi(field, pilot, runwayName string) string {
	field = PronounceATC(field)
	rw := SpeakRunway(runwayName)
	rw = strings.TrimPrefix(rw, "runway ")
	return fmt.Sprintf("%s Ground to %s, taxi to runway %s, veeah taxi way alpha and hold short",
		field, speakCallsign(pilot), rw)
}

func speakCallsign(pilot string) string {
	pilot = SpeakCallsign(pilot)
	// "Viper 1-1" → "Viper one one"
	var b strings.Builder
	for i, r := range pilot {
		if unicode.IsDigit(r) {
			if w, ok := digits[r]; ok {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(w)
				continue
			}
		}
		if r == '-' {
			b.WriteByte(' ')
			continue
		}
		if i > 0 && unicode.IsDigit(rune(pilot[i-1])) && unicode.IsLetter(r) {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
