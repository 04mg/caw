package prefs

import (
	"encoding/json"
	"strconv"

	"github.com/04mg/caw/internal/state"
)

// PetsConfig holds the Petdex desktop-pets settings shared across all
// devices: whether pets are enabled, the ordered roster of pet slugs used
// for rotation, per-agent pinned pets (agentId -> pet slug) and the
// automatically-maintained per-agent assignments that survive a terminal
// being closed and reopened.
type PetsConfig struct {
	Enabled        bool              `json:"enabled"`
	Roster         []string          `json:"roster"`
	AgentPins      map[string]string `json:"agentPins"`
	Assignments    map[string]string `json:"assignments"`
	UniquePerAgent bool              `json:"uniquePerAgent"`
}

type TerminalBackground struct {
	AssetID     string  `json:"assetId"`
	Overlay     float64 `json:"overlay"`
	Blur        float64 `json:"blur"`
	ApplyToPage bool    `json:"applyToPage"`
}

type ColorSchemes struct {
	Dark  map[string]string `json:"dark"`
	Light map[string]string `json:"light"`
}

func (c *ColorSchemes) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := raw["dark"]; ok {
		type colorSchemes ColorSchemes
		var schemes colorSchemes
		if err := json.Unmarshal(data, &schemes); err != nil {
			return err
		}
		*c = ColorSchemes(schemes)
		return nil
	}

	var legacy map[string]string
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	c.Dark = legacy
	c.Light = map[string]string{}
	return nil
}

type CustomizationState struct {
	Version int          `json:"version"`
	UITheme string       `json:"uiTheme"`
	Colors  ColorSchemes `json:"colors"`
	Editor  struct {
		Theme            string            `json:"theme"`
		FontSize         int               `json:"fontSize"`
		Minimap          bool              `json:"minimap"`
		TokenColors      map[string]string `json:"tokenColors"`
		TokenColorsLight map[string]string `json:"tokenColorsLight"`
	} `json:"editor"`
	Terminal struct {
		Theme      string             `json:"theme"`
		FontSize   int                `json:"fontSize"`
		Background TerminalBackground `json:"background"`
	} `json:"terminal"`
	Logo struct {
		Filter string `json:"filter"`
	} `json:"logo"`
	Layout struct {
		SidebarOrder string `json:"sidebarOrder"`
	} `json:"layout"`
}

// DesktopAppUser is a user-defined graphical app entry, mirroring the
// AgentCmds override pattern: users can add custom desktop apps (any
// graphical X11 command) without code changes. The id is the menu label
// key; cmd is the xpra --start-child command. Icon refs are resolved
// client-side: 'si:<slug>' (vendored brand paths), 'lucide:<Name>' or a
// data:image/... upload; IconColor tints vector icons.
type DesktopAppUser struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	Cmd       []string   `json:"cmd"`
	Env       [][]string `json:"env,omitempty"`
	Icon      string     `json:"icon,omitempty"`
	IconColor string     `json:"iconColor,omitempty"`
}

// DesktopStream holds xpra streaming quality preferences, passed to the
// HTML5 client as URL params (encoding/quality/speed).
type DesktopStream struct {
	// Encoding is the picture encoding: auto, jpeg, png or webp.
	Encoding string `json:"encoding"`
	// Quality is the image quality 1-100 (higher = sharper, more bandwidth).
	Quality int `json:"quality"`
	// Speed is the encode speed 1-100 (higher = lower latency, more bandwidth).
	Speed int `json:"speed"`
}

func defaultDesktopStream() DesktopStream {
	return DesktopStream{Encoding: "auto", Quality: 90, Speed: 65}
}

// PrefsState holds the user's work preferences shared across all devices.
type PrefsState struct {
	DefaultNewAgent   string              `json:"defaultNewAgent"`
	DisabledAgents    []string            `json:"disabledAgents"`
	DisabledProviders []string            `json:"disabledProviders"`
	AgentCmds         map[string][]string `json:"agentCmds"`
	DefaultShell      string              `json:"defaultShell"`
	// ParkedTerminals is the number of recently-used terminal xterm.js
	// instances the client keeps mounted in the background so switching back
	// to them is instant (no scrollback replay / WS reconnect). Desktop only.
	ParkedTerminals int                `json:"parkedTerminals"`
	Hotkeys         map[string]string  `json:"hotkeys"`
	Pets            PetsConfig         `json:"pets"`
	Customization   CustomizationState `json:"customization"`
	// DesktopApps holds user-defined graphical applications that appear in
	// the New Tab menu's "Desktop Apps" section alongside the hardcoded
	// defaults. Each entry launches as an xpra --start-child in a desktop
	// leaf. Mirrors the AgentCmds override pattern.
	DesktopApps []DesktopAppUser `json:"desktopApps"`
	// DesktopStream holds xpra streaming quality preferences.
	DesktopStream DesktopStream `json:"desktopStream"`
}

const (
	keyDefaultNewAgent     = "pref_default_new_agent"
	keyDisabledAgents      = "pref_disabled_agents"
	keyDisabledProviders   = "pref_disabled_providers"
	keyAgentCmds           = "pref_agent_cmds"
	keyDefaultShell        = "pref_default_shell"
	keyParkedTerminals     = "pref_parked_terminals"
	keyHotkeys             = "pref_hotkeys"
	keyPets                = "pref_pets"
	keyCustomization       = "pref_customization"
	keyDesktopApps         = "pref_desktop_apps"
	keyDesktopStream       = "pref_desktop_stream"
	defaultParkedTerminals = 6
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

func defaultCustomization() CustomizationState {
	var c CustomizationState
	c.Version = 1
	c.UITheme = "Caw Dark"
	c.Colors = ColorSchemes{Dark: map[string]string{}, Light: map[string]string{}}
	c.Editor.Theme, c.Editor.FontSize, c.Editor.Minimap = "dark", 12, true
	c.Editor.TokenColors = map[string]string{}
	c.Terminal.Theme, c.Terminal.FontSize = "dark", 13
	c.Terminal.Background.Overlay = 0.35
	c.Logo.Filter = "brightness(0) invert(0.55) opacity(0.2)"
	c.Layout.SidebarOrder = "workspace-explorer"
	return c
}

func defaultPrefs() PrefsState {
	return PrefsState{
		DefaultNewAgent:   "none",
		DisabledAgents:    []string{},
		DisabledProviders: []string{},
		AgentCmds:         map[string][]string{},
		DefaultShell:      "",
		ParkedTerminals:   defaultParkedTerminals,
		Hotkeys:           defaultHotkeys(),
		Pets: PetsConfig{
			Enabled:     false,
			Roster:      []string{},
			AgentPins:   map[string]string{},
			Assignments: map[string]string{},
		},
		Customization: defaultCustomization(),
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
	if v, err := store.GetSetting(keyDisabledProviders); err == nil && v != "" {
		var list []string
		if json.Unmarshal([]byte(v), &list) == nil {
			p.DisabledProviders = list
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
	if v, err := store.GetSetting(keyParkedTerminals); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			p.ParkedTerminals = n
		}
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
	if v, err := store.GetSetting(keyCustomization); err == nil && v != "" {
		var customization CustomizationState
		if json.Unmarshal([]byte(v), &customization) == nil {
			if customization.Version == 0 {
				customization = defaultCustomization()
			}
			if customization.Colors.Dark == nil {
				customization.Colors.Dark = map[string]string{}
			}
			if customization.Colors.Light == nil {
				customization.Colors.Light = map[string]string{}
			}
			if customization.Editor.TokenColors == nil {
				customization.Editor.TokenColors = map[string]string{}
			}
			if customization.Editor.TokenColorsLight == nil {
				customization.Editor.TokenColorsLight = map[string]string{}
			}
			if customization.Layout.SidebarOrder == "" {
				customization.Layout.SidebarOrder = "workspace-explorer"
			}
			if customization.Logo.Filter == "" {
				customization.Logo.Filter = defaultCustomization().Logo.Filter
			}
			p.Customization = customization
		}
	}
	if v, err := store.GetSetting(keyDesktopApps); err == nil && v != "" {
		var apps []DesktopAppUser
		if json.Unmarshal([]byte(v), &apps) == nil {
			p.DesktopApps = apps
		}
	}
	p.DesktopStream = defaultDesktopStream()
	if v, err := store.GetSetting(keyDesktopStream); err == nil && v != "" {
		var s DesktopStream
		if json.Unmarshal([]byte(v), &s) == nil {
			if s.Encoding != "" {
				p.DesktopStream.Encoding = s.Encoding
			}
			if s.Quality >= 1 && s.Quality <= 100 {
				p.DesktopStream.Quality = s.Quality
			}
			if s.Speed >= 1 && s.Speed <= 100 {
				p.DesktopStream.Speed = s.Speed
			}
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
	disabledProvidersJSON, _ := json.Marshal(p.DisabledProviders)
	if err := store.SetSetting(keyDisabledProviders, string(disabledProvidersJSON)); err != nil {
		return err
	}
	cmdsJSON, _ := json.Marshal(p.AgentCmds)
	if err := store.SetSetting(keyAgentCmds, string(cmdsJSON)); err != nil {
		return err
	}
	if err := store.SetSetting(keyDefaultShell, p.DefaultShell); err != nil {
		return err
	}
	if err := store.SetSetting(keyParkedTerminals, strconv.Itoa(p.ParkedTerminals)); err != nil {
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
	customizationJSON, _ := json.Marshal(p.Customization)
	if err := store.SetSetting(keyCustomization, string(customizationJSON)); err != nil {
		return err
	}
	desktopAppsJSON, _ := json.Marshal(p.DesktopApps)
	if err := store.SetSetting(keyDesktopApps, string(desktopAppsJSON)); err != nil {
		return err
	}
	streamJSON, _ := json.Marshal(p.DesktopStream)
	if err := store.SetSetting(keyDesktopStream, string(streamJSON)); err != nil {
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

	for agentID, slug := range p.Pets.Assignments {
		if !keep(slug) {
			delete(p.Pets.Assignments, agentID)
			changed = true
		}
	}

	if changed {
		_ = SetPrefs(store, p)
	}
}
