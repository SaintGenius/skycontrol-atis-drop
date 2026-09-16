package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/skycontrol/skycontrol/internal/radio"
	"github.com/skycontrol/skycontrol/internal/voice"
)

func (a *App) runConsole(ctx context.Context) {
	sc := bufio.NewScanner(os.Stdin)
	printPrompt()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if !sc.Scan() {
			return
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			printPrompt()
			continue
		}
		cmd, rest, _ := strings.Cut(line, " ")
		cmd = strings.ToLower(cmd)
		switch cmd {
		case "help", "?", "h":
			printHelp()
		case "quit", "exit", "q":
			fmt.Println("  bye")
			return
		case "status":
			total, gnd, air := a.tower.Stats()
			fmt.Printf("  airfields=%d  aircraft=%d (ground=%d air=%d)\n",
				a.airfields.Count(), total, gnd, air)
			fmt.Printf("  wind: %s\n", a.tower.WindStatus())
		case "wind":
			fmt.Printf("  wind: %s\n", a.tower.WindStatus())
			st := a.tower.PrimaryAircraft()
			if st != nil && st.Nearest != nil {
				fmt.Printf("  nearest field %s  heading=%.0f  (active uses DCS wind if listed above)\n",
					st.Nearest.Name, st.Heading)
			}
		case "atis":
			if a.atis == nil {
				fmt.Println("  ATIS off (no SRS)")
				break
			}
			fmt.Printf("  %s\n", a.atis.Status())
			st := a.tower.PrimaryAircraft()
			if st != nil && st.Nearest != nil {
				fmt.Printf("  field=%s  tower=%s  atis=%s\n",
					st.Nearest.Name,
					st.Nearest.PrimaryTowerFreq(),
					st.Nearest.PrimaryATISFreq())
			}
		case "who":
			st := a.tower.PrimaryAircraft()
			if st == nil {
				fmt.Println("  no aircraft yet — sit in the jet a few seconds")
				break
			}
			nearest := "(none)"
			if st.Nearest != nil {
				nearest = st.Nearest.Name
			}
			fmt.Printf("  name=%s  type=%s\n", st.Callsign, st.Type)
			fmt.Printf("  lat=%.6f  lon=%.6f  alt=%.0fft  hdg=%.0f\n",
				st.Latitude, st.Longitude, st.AltitudeFt, st.Heading)
			fmt.Printf("  nearest=%s  dist=%.1f nm  ground=%v\n", nearest, st.DistanceNM, st.OnGround)
			fmt.Printf("  wind: %s\n", a.tower.WindStatus())
		case "hear":
			text := strings.TrimSpace(rest)
			if text == "" {
				text = "Genius 1-1, Senaki Ground, taxi to runway zero-niner via alpha, hold short."
			}
			fmt.Printf("  playing: %s\n", text)
			path, err := a.speaker.SayTempFile(text)
			if err != nil {
				fmt.Printf("  tts failed: %v\n", err)
				break
			}
			if path == "" {
				fmt.Println("  tts produced no file (voice is still stub)")
				break
			}
			if err := voice.PlayWAV(path); err != nil {
				fmt.Printf("  play failed: %v\n", err)
			} else {
				fmt.Println("  done")
			}
			_ = os.Remove(path)
		case "maps":
			seen := map[string]int{}
			for _, af := range a.airfields.All() {
				seen[af.Map]++
			}
			for m, n := range seen {
				fmt.Printf("  %-16s %d airfields\n", m, n)
			}
		case "airfields", "af":
			filter := strings.ToLower(rest)
			count := 0
			for _, af := range a.airfields.All() {
				if filter != "" &&
					!strings.Contains(strings.ToLower(af.Name), filter) &&
					!strings.Contains(strings.ToLower(af.Map), filter) &&
					!strings.Contains(strings.ToLower(af.ID), filter) {
					continue
				}
				fmt.Printf("  %-18s %-16s tower=%-10s runways=%d\n",
					af.Name, af.Map, af.PrimaryTowerFreq(), len(af.Runways))
				count++
			}
			fmt.Printf("  (%d shown)\n", count)
		case "seed":
			parts := strings.Fields(rest)
			afID := "nellis"
			pilot := "Viper 1-1"
			if len(parts) >= 1 {
				afID = parts[0]
			}
			if len(parts) >= 2 {
				pilot = strings.Join(parts[1:], " ")
			}
			if err := a.tower.SeedDemoAircraft(afID, pilot); err != nil {
				fmt.Printf("  seed failed: %v\n", err)
			} else {
				fmt.Printf("  YOU are now %s at %s\n", pilot, afID)
			}
		default:
			a.injectPilot(line)
		}
		printPrompt()
	}
}

func (a *App) injectPilot(phrase string) {
	pilot := "Aircraft"
	if st := a.tower.PrimaryAircraft(); st != nil {
		if st.Callsign != "" {
			pilot = st.Callsign
		} else if st.Pilot != "" {
			pilot = st.Pilot
		}
	}
	fmt.Printf("  >> pilot: %s\n", phrase)
	a.tower.HandleRadioCall(radio.ReceivedCall{
		Pilot:      pilot,
		Transcript: phrase,
		ReceivedAt: time.Now(),
	})
}

func printPrompt() {
	fmt.Println("skycontrol>")
	_ = os.Stdout.Sync()
}

func printHelp() {
	fmt.Print(`
  Type like you are on the radio:
    request taxi
    ready for departure
    inbound
    say again
    ground say again
    tower say again

  Other:
    seed bagram Gunfighter 1-1
    airfields
    maps
    status
    wind
    atis
    hear
    who
    listen on   (mic starts automatically; this is a reminder)
    help
    quit
`)
}
