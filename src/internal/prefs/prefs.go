package prefs

import (
	"encoding/json"

	"github.com/04mg/caw/internal/state"
)

// PetsConfig holds the Petdex desktop-pets settings shared across all
// devices: whether pets are enabled, the ordered roster of pet slugs used
// for rotation, and per-agent pinned pets (agentId -> pet slug).
type PetsConfig struct {
	Enabled   bool              `json:"enabled"`
	Roster    []string          `json:"roster"`
	AgentPins map[string]string `json:"agentPins"`
}

// PrefsState holds the user's work preferences shared across all devices.
type PrefsState struct {
	DefaultNewAgent string              `json:"defaultNewAgent"`
	DisabledAgents  []string            `json:"disabledAgents"`
	AgentCmds       map[string][]string `json:"agentCmds"`
	DefaultShell    string              `json:"defaultShell"`
	Hotkeys         map[string]string   `json:"hotkeys"`
	Pets            PetsConfig          `json:"pets"`
}

const (
	keyDefaultNewAgent = "pref_default_new_agent"
	keyDisabledAgents  = "pref_disabled_agents"
	keyAgentCmds       = "pref_agent_cmds"
	keyDefaultShell    = "pref_default_shell"
	keyHotkeys         = "pref_hotkeys"
	keyPets            = "pref_pets"
)

func defaultHotkeys() map[string]string {
	return map[string]string{
		"closePane":         "Alt+W",
		"switchPaneLeft":    "Alt+ArrowLeft",
		"switchPaneRight":   "Alt+ArrowRight",
		"newTerminal":       "Alt+T",
		"splitHorizontal":   "Alt+H",
		"splitVertical":     "Alt+V",
		"commandPalette":    "Alt+P",
		"commandPaletteCmd": "Alt+Shift+P",
		"toggleKanban":      "Alt+C",
	}
}

func defaultPrefs() PrefsState {
	return PrefsState{
		DefaultNewAgent: "none",
		DisabledAgents:  []string{},
		AgentCmds:       map[string][]string{},
		DefaultShell:    "",
		Hotkeys:         defaultHotkeys(),
		Pets: PetsConfig{
			Enabled:   false,
			Roster:    []string{},
			AgentPins: map[string]string{},
		},
	}
}

// GetPrefs reads all shared preferences from the settings KV table.
func GetPrefs(store *state.Store) PrefsState {
	p := defaultPrefs()

	if v, err := store.GetSetting(keyDefaultNewAgent); err == nil && v != "" {
		p.DefaultNewAgent = v
	}
	if v, err := store.GetSetting(keyDisabledAgents); err == nil && v != "" {
		var list []string
		if json.Unmarshal([]byte(v), &list) == nil {
			p.DisabledAgents = list
		}
	}
	if v, err := store.GetSetting(keyAgentCmds); err == nil && v != "" {
		var cmds map[string][]string
		if json.Unmarshal([]byte(v), &cmds) == nil {
			p.AgentCmds = cmds
		}
	}
	if v, err := store.GetSetting(keyDefaultShell); err == nil && v != "" {
		p.DefaultShell = v
	}
	if v, err := store.GetSetting(keyHotkeys); err == nil && v != "" {
		var hk map[string]string
		if json.Unmarshal([]byte(v), &hk) == nil {
			// Merge saved hotkeys over defaults so new actions get defaults
			for k, val := range hk {
				p.Hotkeys[k] = val
			}
		}
	}
	if v, err := store.GetSetting(keyPets); err == nil && v != "" {
		var pets PetsConfig
		if json.Unmarshal([]byte(v), &pets) == nil {
			p.Pets = pets
		}
	}

	return p
}

// SetPrefs persists all shared preferences to the settings KV table.
func SetPrefs(store *state.Store, p PrefsState) error {
	if err := store.SetSetting(keyDefaultNewAgent, p.DefaultNewAgent); err != nil {
		return err
	}
	disabledJSON, _ := json.Marshal(p.DisabledAgents)
	if err := store.SetSetting(keyDisabledAgents, string(disabledJSON)); err != nil {
		return err
	}
	cmdsJSON, _ := json.Marshal(p.AgentCmds)
	if err := store.SetSetting(keyAgentCmds, string(cmdsJSON)); err != nil {
		return err
	}
	if err := store.SetSetting(keyDefaultShell, p.DefaultShell); err != nil {
		return err
	}
	hotkeysJSON, _ := json.Marshal(p.Hotkeys)
	if err := store.SetSetting(keyHotkeys, string(hotkeysJSON)); err != nil {
		return err
	}
	petsJSON, _ := json.Marshal(p.Pets)
	if err := store.SetSetting(keyPets, string(petsJSON)); err != nil {
		return err
	}
	return nil
}

// PrunePets removes every slug rejected by keep from the shared pets config
// (roster entries and agent pins). Used when an uploaded pet is deleted so
// stale slugs never linger in prefs.
func PrunePets(store *state.Store, keep func(slug string) bool) {
	p := GetPrefs(store)
	changed := false

	roster := p.Pets.Roster[:0]
	for _, slug := range p.Pets.Roster {
		if keep(slug) {
			roster = append(roster, slug)
		} else {
			changed = true
		}
	}
	p.Pets.Roster = roster

	for agentID, slug := range p.Pets.AgentPins {
		if !keep(slug) {
			delete(p.Pets.AgentPins, agentID)
			changed = true
		}
	}

	if changed {
		_ = SetPrefs(store, p)
	}
}
