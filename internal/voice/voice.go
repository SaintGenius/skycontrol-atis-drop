package voice

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/skycontrol/skycontrol/internal/atc"
)

type Speaker interface {
	Say(text string) ([]float32, error)
	SayToFile(text, outPath string) error
	SayTempFile(text string) (path string, err error)
	Name() string
}

type Config struct {
	Provider string
	Voice    string
	Speed    float64
	PiperBin string
	ModelDir string
	Log      *slog.Logger
}

func New(cfg Config) (Speaker, error) {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	switch strings.ToLower(cfg.Provider) {
	case "piper":
		s, err := NewPiperSpeaker(cfg)
		if err != nil {
			log.Warn("piper not available, using stub voice", "error", err)
			return NewStubSpeaker(cfg.Voice, log), nil
		}
		return s, nil
	case "stub", "":
		return NewStubSpeaker(cfg.Voice, log), nil
	default:
		log.Warn("unknown voice provider, using stub", "provider", cfg.Provider)
		return NewStubSpeaker(cfg.Voice, log), nil
	}
}

type StubSpeaker struct {
	name string
	log  *slog.Logger
}

func NewStubSpeaker(name string, log *slog.Logger) *StubSpeaker {
	if name == "" {
		name = "american-military-male (stub)"
	}
	return &StubSpeaker{name: name, log: log}
}

func (s *StubSpeaker) Name() string { return s.name }

func (s *StubSpeaker) Say(text string) ([]float32, error) {
	s.log.Info("TTS (stub)", "voice", s.name, "text", text)
	return make([]float32, 16000), nil
}

func (s *StubSpeaker) SayToFile(text, outPath string) error {
	s.log.Info("TTS (stub) would write file", "voice", s.name, "text", text, "path", outPath)
	return nil
}

func (s *StubSpeaker) SayTempFile(text string) (string, error) {
	s.log.Info("TTS (stub) temp file", "voice", s.name, "text", text)
	return "", nil
}

type PiperSpeaker struct {
	cfg      Config
	log      *slog.Logger
	mu       sync.Mutex
	piperBin string
	model    string
	config   string
}

func NewPiperSpeaker(cfg Config) (*PiperSpeaker, error) {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	bin := cfg.PiperBin
	if bin == "" {
		for _, c := range []string{
			filepath.Join("piper", "piper.exe"),
			filepath.Join("piper", "piper"),
			"piper.exe",
		} {
			if _, err := os.Stat(c); err == nil {
				bin = c
				break
			}
		}
		if bin == "" {
			if p, err := exec.LookPath("piper"); err == nil {
				bin = p
			} else if p, err := exec.LookPath("piper.exe"); err == nil {
				bin = p
			} else {
				return nil, fmt.Errorf("piper binary not found (looked in piper\\piper.exe and PATH)")
			}
		}
	}
	modelDir := cfg.ModelDir
	if modelDir == "" {
		modelDir = "data/voices"
	}
	voice := cfg.Voice
	if voice == "" || voice == "american-military-male" {
		voice = "en_US-hfc_male-medium"
	}
	model := filepath.Join(modelDir, voice+".onnx")
	configPath := model + ".json"
	if _, err := os.Stat(model); err != nil {
		return nil, fmt.Errorf("model not found: %s", model)
	}
	return &PiperSpeaker{
		cfg:      cfg,
		log:      log,
		piperBin: bin,
		model:    model,
		config:   configPath,
	}, nil
}

func (s *PiperSpeaker) Name() string { return s.cfg.Voice }

func (s *PiperSpeaker) Say(text string) ([]float32, error) {
	tmp := filepath.Join(os.TempDir(), "skycontrol-tts.wav")
	if err := s.SayToFile(text, tmp); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *PiperSpeaker) SayToFile(text, outPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	text = normalizeAviation(text)
	text = atc.PronounceATC(text)
	args := []string{"--model", s.model, "--output_file", outPath, "--sentence_silence", "0.55"}
	if s.config != "" {
		if _, err := os.Stat(s.config); err == nil {
			args = append(args, "--config", s.config)
		}
	}
	if s.cfg.Speed > 0 && s.cfg.Speed != 1.0 {
		args = append(args, "--length_scale", fmt.Sprintf("%.2f", 1.0/s.cfg.Speed))
	}
	cmd := exec.Command(s.piperBin, args...)
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	hidePiperConsole(cmd)
	s.log.Debug("running piper", "bin", s.piperBin, "model", s.model, "text", text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("piper: %w", err)
	}
	return nil
}

func (s *PiperSpeaker) SayTempFile(text string) (string, error) {
	f, err := os.CreateTemp("", "skycontrol-tts-*.wav")
	if err != nil {
		return "", err
	}
	path := f.Name()
	_ = f.Close()
	if err := s.SayToFile(text, path); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func normalizeAviation(text string) string {
	replacer := strings.NewReplacer(
		"RWY", "runway",
		"rwy", "runway",
		"ILS", "I L S",
		"TACAN", "tackan",
		"ATIS", "A T I S",
		"GCI", "G C I",
		"AWACS", "A wax",
	)
	return replacer.Replace(text)
}

var RecommendedVoices = []string{
	"en_US-hfc_male-medium",
	"en_US-joe-medium",
	"en_US-john-medium",
	"en_US-ryan-medium",
	"en_US-bryce-medium",
}

// PlayWAV plays a WAV on the default Windows speakers (blocking).
func PlayWAV(path string) error {
	if path == "" {
		return fmt.Errorf("empty wav path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	esc := strings.ReplaceAll(abs, "'", "''")
	ps := fmt.Sprintf("$p = New-Object System.Media.SoundPlayer '%s'; $p.PlaySync()", esc)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-Command", ps)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("play wav: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
