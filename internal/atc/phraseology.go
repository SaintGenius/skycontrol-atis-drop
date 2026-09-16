package atc

import "strings"

// Intent is a recognized pilot request.
type Intent string

const (
	IntentUnknown      Intent = ""
	IntentTakeoff      Intent = "takeoff"
	IntentLanding      Intent = "landing"
	IntentTaxi         Intent = "taxi"
	IntentStartup      Intent = "startup"
	IntentRadioCheck   Intent = "radio_check"
	IntentGoAround     Intent = "go_around"
	IntentHoldShort    Intent = "hold_short"
	IntentInbound      Intent = "inbound"
	IntentTouchAndGo   Intent = "touch_and_go"
	IntentParking      Intent = "parking"
	IntentSayAgain     Intent = "say_again"
	IntentContact      Intent = "contact"
	IntentCheckIn      Intent = "check_in"
	IntentUnable       Intent = "unable"
	IntentTraffic      Intent = "traffic"
	IntentATIS         Intent = "atis"
)

// DetectIntent maps a transcript to a Tower intent.
func DetectIntent(text string) Intent {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return IntentUnknown
	}

	switch {
	case t == "again" || t == "say again" || t == "see again" || t == "saying again" ||
		containsAny(t, "say again", "see again", "saying again", "say-again",
			"say last", "last transmission", "repeat last", "repeat",
			"say that again", "come again", "didn't copy", "did not copy",
			"say again last", "last rans", "rans mission"):
		return IntentSayAgain
	case containsAny(t, "unable", "stay this frequency", "remain this frequency",
		"staying this frequency", "negative switch", "staying with you", "remain with you"):
		return IntentUnable
	case containsAny(t, "radio check", "how do you hear", "comm check", "checking in", "check in"):
		return IntentRadioCheck
	case containsAny(t, "any traffic", "call traffic", "traffic in the area", "do you have traffic"):
		return IntentTraffic
	case containsAny(t, "atis", "a t i s", "a tis", "adis", "a dis", "addis",
		"eighty s", "80s", "eighty is",
		"information frequency", "atis frequency", "request atis", "copy atis",
		"what is atis", "what's atis", "atis freq", "adis frequency"):
		return IntentATIS
	case containsAny(t, "switch to", "switching to", "contacting", "contact ", "change to", "changing to"):
		return IntentContact
	case containsAny(t, "go around", "going around", "missed approach", "and around", "go round"):
		return IntentGoAround
	case containsAny(t, "touch and go", "touch-and-go", "the option"):
		return IntentTouchAndGo
	case containsAny(t, "request parking", "taxi to parking", "request to park"):
		return IntentParking
	case containsAny(t, "hold short"):
		return IntentHoldShort
	case containsAny(t, "request startup", "requesting startup", "request start", "requesting start",
		"ready to start", "request engine start", "engine start", "start engines",
		"start my aircraft", "start the aircraft", "would like to start", "want to start"):
		return IntentStartup
	case containsAny(t, "request takeoff", "requesting takeoff", "ready for departure", "ready for takeoff",
		"request departure", "cleared for takeoff", "line up", "lining up"):
		return IntentTakeoff
	case containsAny(t, "request landing", "requesting landing", "request approach", "full stop",
		"cleared to land", "request to land"):
		return IntentLanding
	case containsAny(t, "inbound", "in bound", "request inbound", "calling inbound",
		"ten miles", "establishing", "on final", "final"):
		return IntentInbound
	case containsAny(t, "request taxi", "requesting taxi", "need a taxi", "need taxi", "taxi to", "ready to taxi"):
		return IntentTaxi
	default:
		return IntentUnknown
	}
}
