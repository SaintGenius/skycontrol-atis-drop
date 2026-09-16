package airfield

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Database holds all loaded airfields and provides lookup methods.
type Database struct {
	mu        sync.RWMutex
	airfields map[string]*Airfield // key = ID (lowercase)
	byMap     map[string][]*Airfield
	all       []*Airfield
}

// NewDatabase creates an empty database.
func NewDatabase() *Database {
	return &Database{
		airfields: make(map[string]*Airfield),
		byMap:     make(map[string][]*Airfield),
		all:       make([]*Airfield, 0),
	}
}

// LoadDir loads all *.json files from the given directory.
func (db *Database) LoadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading airfield directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := db.LoadFile(path); err != nil {
			return fmt.Errorf("loading %s: %w", path, err)
		}
	}
	db.fillMissingATIS()
	return nil
}

// LoadFile loads a single map JSON file into the database.
func (db *Database) LoadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var mf MapFile
	if err := json.Unmarshal(data, &mf); err != nil {
		return fmt.Errorf("parsing JSON: %w", err)
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	for i := range mf.Airfields {
		af := &mf.Airfields[i]

		// Ensure map is set
		if af.Map == "" {
			af.Map = mf.Map
		}

		// Normalize ID
		if af.ID == "" {
			af.ID = strings.ToLower(strings.ReplaceAll(af.Name, " ", "_"))
		}
		af.ID = strings.ToLower(af.ID)

		db.airfields[af.ID] = af
		db.byMap[af.Map] = append(db.byMap[af.Map], af)
		db.all = append(db.all, af)
	}

	return nil
}

func (db *Database) fillMissingATIS() {
	db.mu.Lock()
	defer db.mu.Unlock()

	for _, list := range db.byMap {
		used := map[int]struct{}{
			121500: {},
			243000: {},
			305000: {},
		}
		for _, af := range list {
			markFreqs(used, af.Frequencies.Tower)
			markFreqs(used, af.Frequencies.Ground)
			markFreqs(used, af.Frequencies.Approach)
			markFreqs(used, af.Frequencies.Departure)
			markFreqs(used, af.Frequencies.ATIS)
		}
		for _, af := range list {
			if len(af.Frequencies.ATIS) > 0 {
				continue
			}
			base, ok := primaryUHFkHz(af)
			if !ok {
				continue
			}
			cand := pickATIS(base, used)
			if cand == 0 {
				continue
			}
			af.Frequencies.ATIS = []string{formatFreqkHz(cand)}
			used[cand] = struct{}{}
		}
	}
}

func markFreqs(used map[int]struct{}, list []string) {
	for _, s := range list {
		if k, ok := parseFreqkHz(s); ok {
			used[k] = struct{}{}
		}
	}
}

func primaryUHFkHz(af *Airfield) (int, bool) {
	var first int
	var haveFirst bool
	for _, s := range af.Frequencies.Tower {
		k, ok := parseFreqkHz(s)
		if !ok {
			continue
		}
		if !haveFirst {
			first, haveFirst = k, true
		}
		if k >= 200000 {
			return k, true
		}
	}
	return first, haveFirst
}

func pickATIS(base int, used map[int]struct{}) int {
	uhf := base >= 200000
	offsets := []int{-100, 100, -200, 200, -50, 50, -150, 150, -250, 250, -300, 300, -25, 25, -75, 75, -125, 125, -175, 175, -350, 350, -400, 400}
	for _, o := range offsets {
		c := base + o
		if c%25 != 0 {
			continue
		}
		if uhf {
			if c < 225000 || c > 399975 {
				continue
			}
		} else if c < 118000 || c > 136975 {
			continue
		}
		if _, taken := used[c]; taken {
			continue
		}
		return c
	}
	return 0
}

func parseFreqkHz(s string) (int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.ToUpper(s), "AM")
	s = strings.TrimSuffix(strings.ToUpper(s), "FM")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	mhz, err := strconv.ParseFloat(s, 64)
	if err != nil || mhz < 1 {
		return 0, false
	}
	return int(math.Round(mhz * 1000)), true
}

func formatFreqkHz(kHz int) string {
	return fmt.Sprintf("%.3f", float64(kHz)/1000.0)
}

// GetByID returns an airfield by its stable ID.
func (db *Database) GetByID(id string) (*Airfield, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	af, ok := db.airfields[strings.ToLower(id)]
	return af, ok
}

// GetByName returns the first airfield matching the short name (case-insensitive).
func (db *Database) GetByName(name string) (*Airfield, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	name = strings.ToLower(name)
	for _, af := range db.all {
		if strings.ToLower(af.Name) == name {
			return af, true
		}
	}
	return nil, false
}

// GetByMap returns all airfields on a given map.
func (db *Database) GetByMap(mapName string) []*Airfield {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.byMap[mapName]
}

// All returns a copy of all loaded airfields.
func (db *Database) All() []*Airfield {
	db.mu.RLock()
	defer db.mu.RUnlock()
	result := make([]*Airfield, len(db.all))
	copy(result, db.all)
	return result
}

// Count returns the total number of loaded airfields.
func (db *Database) Count() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.all)
}

// Nearest returns the closest airfield to the given coordinates.
// Distance is approximate (haversine) in nautical miles.
func (db *Database) Nearest(lat, lon float64) (*Airfield, float64) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if len(db.all) == 0 {
		return nil, 0
	}

	var nearest *Airfield
	minDist := math.MaxFloat64

	for _, af := range db.all {
		d := haversineNM(lat, lon, af.Latitude, af.Longitude)
		if d < minDist {
			minDist = d
			nearest = af
		}
	}
	return nearest, minDist
}

// NearestOnMap returns the closest airfield on a specific map.
func (db *Database) NearestOnMap(mapName string, lat, lon float64) (*Airfield, float64) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	list := db.byMap[mapName]
	if len(list) == 0 {
		return nil, 0
	}

	var nearest *Airfield
	minDist := math.MaxFloat64

	for _, af := range list {
		d := haversineNM(lat, lon, af.Latitude, af.Longitude)
		if d < minDist {
			minDist = d
			nearest = af
		}
	}
	return nearest, minDist
}

// PrimaryTowerFreq returns the first Tower frequency, or empty string.
func (af *Airfield) PrimaryTowerFreq() string {
	if len(af.Frequencies.Tower) > 0 {
		return af.Frequencies.Tower[0]
	}
	return ""
}

// PrimaryGroundFreq returns the first Ground frequency, or empty string.
func (af *Airfield) PrimaryGroundFreq() string {
	if len(af.Frequencies.Ground) > 0 {
		return af.Frequencies.Ground[0]
	}
	return ""
}

func (af *Airfield) PrimaryATISFreq() string {
	if af == nil {
		return ""
	}
	if len(af.Frequencies.ATIS) > 0 {
		return af.Frequencies.ATIS[0]
	}
	return ""
}

// Callsign returns a proper callsign for the given role.
// Example: "Nellis Tower"
func (af *Airfield) Callsign(role Role) string {
	if af == nil || af.Name == "" {
		return string(role)
	}
	return af.Name + " " + string(role)
}

// haversineNM calculates great-circle distance in nautical miles.
func haversineNM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusNM = 3440.065 // Earth radius in nautical miles

	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	deltaLat := (lat2 - lat1) * math.Pi / 180
	deltaLon := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusNM * c
}
