package atc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/skycontrol/skycontrol/internal/airfield"
)

type AgentConfig struct {
	Enabled bool
	APIKey  string
	BaseURL string
	Model   string
	Log     *slog.Logger
}

type Agent struct {
	cfg    AgentConfig
	client *http.Client
	log    *slog.Logger
}

type Snapshot struct {
	Transcript     string             `json:"transcript"`
	Pilot          string             `json:"pilot"`
	Callsign       string             `json:"callsign"`
	Type           string             `json:"aircraft_type"`
	Airfield       string             `json:"airfield"`
	ICAO           string             `json:"icao"`
	TowerCallsign  string             `json:"tower_callsign"`
	GroundCallsign string             `json:"ground_callsign"`
	Runway         string             `json:"runway_spoken"`
	Runways        string             `json:"runways"`
	OnGround       bool               `json:"on_ground"`
	AltitudeFt     float64            `json:"altitude_ft"`
	Heading        float64            `json:"heading"`
	DistanceNM     float64            `json:"distance_nm"`
	SpeedKt        float64            `json:"speed_kt"`
	GearDown       bool               `json:"gear_down"`
	Phase          string             `json:"phase,omitempty"`
	PatternLeg     string             `json:"pattern_leg,omitempty"`
	ClearedLand    bool               `json:"cleared_to_land,omitempty"`
	ClearedTakeoff bool               `json:"cleared_for_takeoff,omitempty"`
	Emergency      bool               `json:"emergency,omitempty"`
	EmergKind      string             `json:"emergency_kind,omitempty"`
	Souls          int                `json:"souls_on_board,omitempty"`
	Throttle       float64            `json:"throttle,omitempty"`
	Flaps          float64            `json:"flaps,omitempty"`
	EngineOff      bool               `json:"engine_off,omitempty"`
	LastClearance  string             `json:"last_atc"`
	TowerFreq      string             `json:"tower_freq"`
	GroundFreq     string             `json:"ground_freq"`
	ApproachFreq   string             `json:"approach_freq"`
	ATISFreq       string             `json:"atis_freq"`
	TACAN          string             `json:"tacan"`
	FieldElevFt    float64            `json:"field_elevation_ft"`
	Nearby         []airfield.Nearby  `json:"nearby_airfields"`
	AskedField     *airfield.Nearby   `json:"asked_field,omitempty"`
	Traffic        []TrafficBrief     `json:"traffic"`
	TrafficCall    string             `json:"traffic_call,omitempty"`
	RunwayClear    bool               `json:"runway_clear"`
	OwnerField     string             `json:"owner_field,omitempty"`
	PendingField   string             `json:"pending_field,omitempty"`
	PendingFreq    string             `json:"pending_freq,omitempty"`
	HandoffPending bool               `json:"handoff_pending,omitempty"`
	WindFromDeg    float64            `json:"wind_from_deg"`
	WindKt         float64            `json:"wind_kt"`
	WindSource     string             `json:"wind_source"`
}

type TrafficBrief struct {
	Callsign    string  `json:"callsign"`
	Type        string  `json:"type"`
	AltitudeFt  float64 `json:"altitude_ft"`
	DistanceNM  float64 `json:"distance_nm"`
	Heading     float64 `json:"heading"`
	OnGround    bool    `json:"on_ground"`
	SpeedKt     float64 `json:"speed_kt"`
	GearDown    bool    `json:"gear_down"`
	Phase       string  `json:"phase,omitempty"`
}

type Decision struct {
	Intent string `json:"intent"`
	Role   string `json:"role"`
	Text   string `json:"text"`
}

func NewAgent(cfg AgentConfig) *Agent {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Agent{
		cfg:    cfg,
		client: &http.Client{Timeout: 6 * time.Second},
		log:    log,
	}
}

func (a *Agent) Enabled() bool {
	return a != nil && a.cfg.Enabled && strings.TrimSpace(a.cfg.APIKey) != ""
}

const agentSystem = `You are a US military ATC controller in DCS World. You have a live Tacview + airfield-database snapshot of THIS pilot. You hear messy speech-to-text. Hunt for meaning. Answer from the snapshot. Brief, professional, a little personality. Never chatty. Never invent numbers.

SNAPSHOT FACTS — read these out when asked:
- heading (STT often hears "adding", "hurting", "head in", "having")
- altitude_ft, distance_nm to YOUR field, on_ground
- speed_kt (ground speed on the pad/taxi, indicated once airborne). 0 + on_ground = parked / chocks. Rolling taxi is usually 12–30 kt. Airborne cruise is hundreds of knots. If on_ground is true, NEVER treat speed_kt as airborne.
- phase: parked, taxi, runway, departure, downwind, base, final, airborne. THIS is how you know if they are sitting, rolling, taking off, in the pattern, or on final. gear_down ALONE is not final — a jet pointing AWAY from the field is departure even with gear down.
- engine_off: true means RPM is ~0 (cold / shutdown). Idle engines are NOT engine_off.
- throttle 0–1 if Tacview sent it. Afterburner/high throttle on the ground near the field is a takeoff roll.
- flaps 0–1 if sent.
- gear_down: true means landing gear is down
- traffic[].phase is the same vocabulary for OTHER aircraft. Do not call phase=departure or phase=parked "on final". Do not call parked jets traffic.
- runway_spoken is THE active runway in use. Use that one for taxi, hold short, takeoff, and landing.
- runways is the same active runway (opposite-end numbers like 09/27 are ONE strip, not two runways)
- wind_from_deg / wind_kt: mission wind (FROM, knots). wind_source "dcs" or "config" means runway_spoken already used DCS's 6-knot rule (under 6 kt = field default; 6+ kt = into the wind). Do not pick a runway from heading when wind_source is dcs or config. "heading" means no DCS wind yet.
- tower_freq, ground_freq, approach_freq, atis_freq, tacan, field_elevation_ft, icao
- If they ask ATIS / ADIS / information / "eighty s" / ATIS frequency: intent "info". Read atis_freq ONLY (never tower_freq). Speak it as digits ("two six zero decimal niner"). Tell them to tune to comm two and copy information. Never say C.O.M. or COM2. Do not taxi them. If atis_freq is empty, say no ATIS this field, stay this frequency.
- nearby_airfields: other fields on this map, sorted nearest-first, with distance_nm, bearing_deg, and tower_freq. Index 0 is usually YOUR field (tiny distance). Index 1 is the next nearest.
- asked_field: if they named a field (Kutaisi, Batumi, Nellis…) this is that field's distance and freqs
- traffic: other LIVE aircraft only (moving or airborne). Parked jets and wrecks are omitted.
- traffic_call: if not empty, a ONE-sentence tower advisory already written from Tacview. For landing, inbound, takeoff, go-around, touch-and-go, or check-in: include that EXACT phrase. Do not invent other traffic. Do not skip it.
- runway_clear: true means the runway is empty. If true, NEVER say occupied, blocked, closed, or unable.
- last_atc

If they ask how far to another field: use asked_field if present, else look up that name in nearby_airfields. Do NOT reuse distance_nm (that is only to YOUR field). Give distance_nm AND bearing_deg ("Kutaisi is two one miles, heading three four zero").
If they ask the next nearest / nearest alternate / next field: use nearby_airfields[1], never [0].
If a freq is empty, say stay this frequency — do not invent one.
If they ask traffic and traffic is empty and traffic_call is empty, say no other traffic observed.

RUNWAYS:
- Taxi TO runway_spoken. Hold short of runway_spoken. Never hold short of the opposite-end number (09 and 27 are the same pavement).
- If they asked a runway that is not in the snapshot, use runway_spoken (e.g. they said 09R and the field only has 09).
- If runway_clear is true, clear them. Do not say the runway is occupied.
- If they asked to land or take off and runway_clear is true, issue the clearance. Parked aircraft on the ramp do not occupy the runway.
- intent "info": answer the question ONLY. Do not add taxi, hold-short, or takeoff unless they asked for that clearance.

OVERHEAD BREAK (fighters). One clearance, not five. Use pattern_leg if set:
- initial / inbound: "report break". Do NOT clear to land.
- break / midfield break: "roger break, report downwind". Do NOT clear to land.
- downwind / gear down: "roger gear, report base". Do NOT clear to land.
- base: NOW "cleared to land" runway_spoken.
- short final: if cleared_to_land, "continue, cleared to land". If not yet cleared, clear them now.
- go-around: cancel landing clearance, re-enter downwind.
- Do not repeat wind on every leg. Wind on initial and on the landing clearance only.

DEPARTURE:
- taxi: taxi to runway_spoken, hold short.
- ready / holding short: wind + cleared for takeoff runway_spoken. One takeoff clearance.
- airborne / climbing: "radar contact, continue climb, remain this frequency" unless they asked to switch.
- Do not clear takeoff again after they are airborne.

COURTESY (after the legal call is done):
- thanks / good day / no further assistance → short "roger, good day." Do NOT recap wind, parking, or clearances.
- Never joke, never meow, never skip a clearance to be friendly. Personality is tone, not standup.

EMERGENCY (mayday, pan-pan, low fuel, bingo, engine out, bird strike):
- Skip the overhead. Do NOT report break. Do NOT send them around.
- Immediately: roger the emergency, runway_spoken, cleared to land, full stop.
- Equipment standing by (not required for pan-pan).
- If souls_on_board is 0, ask "say souls on board" once.
- If they already said low fuel / bingo, do not ask fuel remaining.
- If they later say souls or fuel, acknowledge only. Do not reclear.
- On the ground: hold position, equipment rolling.
- intent "emergency".

You MAY also issue taxi / takeoff / land / go-around / hold short / startup / parking / radio check when they asked.

Handoff:
- You currently ARE owner_field. Speak as that station. Transmit as if they are still on owner_field's frequency.
- If they address a different field by name (e.g. "Minhad Tower checking in") or say they are switching to it: intent "check_in". Speak as THAT field. Do not keep telling them to contact it.
- If handoff_pending is true and they check in, radio check, or inbound without naming the old field: intent "check_in". Speak as pending_field.
- If they name the old owner_field after a contact, stay owner_field (they did not switch).
- Do not invent a handoff. Only contact when they asked or the snapshot already has pending_field.

You may NOT:
- Invent a different airfield, runway, frequency, TACAN, or distance than the snapshot.
- Issue taxi just because on_ground is true.
- Repeat last_atc unless they asked you to say again.
- Roleplay as a copilot, GCI, or friend. You are Ground, Tower, or Approach.

Only intent "unknown" if there is no snapshot question and no clearance request.

JSON only:
{"intent":"taxi|takeoff|landing|inbound|go_around|hold_short|startup|parking|radio_check|touch_and_go|say_again|info|contact|check_in|traffic|thanks|emergency|unknown","role":"Ground|Tower|Approach","text":"..."}

text = one radio transmission. Start with the pilot callsign, then your station. 12–40 words. Speak frequencies as "two six one decimal zero". Speak TACAN as digits plus NATO letters: 31X = "three one x-ray", 16Y = "one six yankee". Never say "ex" or "why". No markdown. No extra keys.`

func (a *Agent) Decide(snap Snapshot) (Decision, error) {
	var zero Decision
	if !a.Enabled() {
		return zero, fmt.Errorf("agent disabled")
	}
	body, _ := json.Marshal(snap)
	payload := map[string]any{
		"model": a.cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": agentSystem},
			{"role": "user", "content": "Tacview snapshot and pilot radio:\n" + string(body)},
		},
		"temperature": 0.3,
		"max_tokens":  280,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return zero, err
	}
	url := strings.TrimRight(a.cfg.BaseURL, "/") + "/chat/completions"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return zero, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, fmt.Errorf("ai http %d: %s", resp.StatusCode, truncate(string(b), 200))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return zero, err
	}
	if len(parsed.Choices) == 0 {
		return zero, fmt.Errorf("empty ai response")
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var d Decision
	if err := json.Unmarshal([]byte(content), &d); err != nil {
		return zero, fmt.Errorf("ai json: %w (%s)", err, truncate(content, 120))
	}
	d.Text = strings.TrimSpace(d.Text)
	d.Intent = strings.ToLower(strings.TrimSpace(d.Intent))
	d.Role = strings.TrimSpace(d.Role)
	if d.Text == "" {
		return zero, fmt.Errorf("ai returned empty text")
	}
	return d, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
