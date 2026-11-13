package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/yetone/magpie/internal/edit"
)

// Jellytool has its own vendor in WorkBuddy's shared picker. Seeing magpie's
// vendor is never permission to adopt, refresh or disconnect that installation.
func jellytoolBuddyKey(path, agent string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			path = "~/" + filepath.ToSlash(rel)
		}
	}
	return agent + ".workbuddy.file.before:" + path
}

func jellytoolBuddyWired(path, agent string) bool {
	if stashLoad()[jellytoolBuddyKey(path, agent)] != "" {
		return true // still connected when the user selects no models
	}
	d, _ := workbuddyRead(path)
	for _, raw := range d.models {
		var e workbuddyEntry
		if json.Unmarshal(raw, &e) == nil && e.Vendor == "jellytool" {
			return true
		}
	}
	return false
}

func jellytoolBuddyStash() (map[string]string, error) {
	m := map[string]string{}
	b, err := os.ReadFile(stashPath())
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]string{}
	}
	return m, nil
}

// Snapshot persistence must succeed before the picker changes; restore points
// remain until the file was successfully restored, including after a failure.
func jellytoolBuddyWrite(path, agent string, on bool) error {
	if !on {
		return jellytoolBuddyRestore(path, agent)
	}
	doc, err := workbuddyRead(path)
	if err != nil {
		return err
	}
	models := magpieModels(agent)
	for _, raw := range doc.models {
		var e workbuddyEntry
		if json.Unmarshal(raw, &e) != nil || e.Vendor == "jellytool" {
			continue
		}
		for _, model := range models {
			if e.ID == model.ID {
				return fmt.Errorf("WorkBuddy already has model %q from another provider", e.ID)
			}
		}
	}
	key := jellytoolBuddyKey(path, agent)
	stashMu.Lock()
	m, err := jellytoolBuddyStash()
	if err == nil && m[key] == "" {
		if len(models) == 0 {
			err = &NoModelsError{Agent: "WorkBuddy"}
		} else {
			var before []byte
			before, err = os.ReadFile(path)
			switch {
			case errors.Is(err, os.ErrNotExist):
				m[key], err = "\x00missing", nil
			case err == nil && len(before) == 0:
				m[key] = "\x00empty"
			case err == nil:
				m[key] = string(before)
			}
			if err == nil {
				err = stashWrite(m)
			}
		}
	}
	stashMu.Unlock()
	if err != nil {
		return err
	}
	return buddyWriteOwned(path, agent, true, place{}, "jellytool")
}

func jellytoolBuddyRestore(path, agent string) error {
	key := jellytoolBuddyKey(path, agent)
	stashMu.Lock()
	m, err := jellytoolBuddyStash()
	before := m[key]
	stashMu.Unlock()
	if err != nil {
		return err
	}
	if err := buddyWriteOwned(path, agent, false, place{}, "jellytool"); err != nil {
		return err
	}
	if before != "" {
		current, err := edit.Read(path)
		if err != nil {
			return err
		}
		d, err := workbuddyRead(path)
		if err != nil {
			return err
		}
		switch {
		case before == "\x00missing" && len(d.models) == 0 && d.rest == nil:
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		case before == "\x00empty" && len(d.models) == 0 && d.rest == nil:
			if err := edit.WriteAtomic(path, nil); err != nil {
				return err
			}
		default:
			// Put formatting back only when removing Jellytool left the same data.
			// Edits made by WorkBuddy, magpie or the user while connected stay theirs.
			var was, now any
			if json.Unmarshal(edit.TrimBOM([]byte(before)), &was) == nil && json.Unmarshal(current, &now) == nil && reflect.DeepEqual(was, now) {
				if err := edit.WriteAtomic(path, []byte(before)); err != nil {
					return err
				}
			}
		}
	}
	stashMu.Lock()
	defer stashMu.Unlock()
	m, err = jellytoolBuddyStash()
	if err != nil {
		return err
	}
	delete(m, key)
	return stashWrite(m)
}
