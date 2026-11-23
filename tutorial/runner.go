package tutorial

import (
	"errors"
	"fmt"
	"strings"

	"ofam.dev/ochazuke/core"
)

// Driver executes tutorial keystrokes against an application and renders frames.
type Driver interface {
	SendKey(key string) error
	RenderANSI() string
	RenderPlain() string
}

// CapturedStep records one executed tutorial step along with its resolved keys
// and rendered terminal frames.
type CapturedStep struct {
	SectionID string   `json:"sectionId"`
	Index     int      `json:"index"` // 1-based global step number across the tutorial
	Caption   string   `json:"caption"`
	Command   string   `json:"command,omitempty"`
	Keys      []string `json:"keys,omitempty"`
	KeyBadge  string   `json:"keyBadge,omitempty"`
	ANSI      string   `json:"ansi"`
	Plain     string   `json:"plain"`
	PauseMs   int      `json:"pauseMs,omitempty"`
	Captured  bool     `json:"captured"`
}

// CapturedSection holds the executed steps for one tutorial section.
type CapturedSection struct {
	ID    string         `json:"id"`
	Title string         `json:"title"`
	Goal  string         `json:"goal,omitempty"`
	Steps []CapturedStep `json:"steps"`
}

// Result is the complete deterministic output of Run.
type Result struct {
	Spec         Spec              `json:"spec"`
	InitialANSI  string            `json:"initialAnsi,omitempty"`
	InitialPlain string            `json:"initialPlain,omitempty"`
	Sections     []CapturedSection `json:"sections"`
}

// Run executes spec headlessly through driver using bindings for command key
// resolution. It captures the initial screen frame before sending keys, then
// executes each section and step in order, checking every Expect substring
// against RenderPlain().
func Run(spec Spec, driver Driver, bindings []core.Binding) (Result, error) {
	if driver == nil {
		return Result{}, errors.New("tutorial run: driver is nil")
	}
	if spec.Width == 0 {
		spec.Width = DefaultWidth
	}
	if spec.Height == 0 {
		spec.Height = DefaultHeight
	}

	res := Result{
		Spec:         spec,
		InitialANSI:  driver.RenderANSI(),
		InitialPlain: driver.RenderPlain(),
		Sections:     make([]CapturedSection, 0, len(spec.Sections)),
	}

	globalIndex := 0
	for _, sec := range spec.Sections {
		csec := CapturedSection{
			ID:    sec.ID,
			Title: sec.Title,
			Goal:  sec.Goal,
			Steps: make([]CapturedStep, 0, len(sec.Steps)),
		}
		for stepIdx, step := range sec.Steps {
			globalIndex++
			keys, badge := ResolveKeys(step, bindings)
			for keyIdx, k := range keys {
				if err := driver.SendKey(k); err != nil {
					return Result{}, fmt.Errorf(
						"tutorial section %q step %d (%q) key %d (%q): %w",
						sec.ID, stepIdx+1, step.Caption, keyIdx+1, k, err,
					)
				}
			}

			ansi := driver.RenderANSI()
			plain := driver.RenderPlain()

			for _, want := range step.Expect {
				if !strings.Contains(plain, want) {
					return Result{}, fmt.Errorf(
						"tutorial assertion failed in section %q step %d (%q): expected %q not found in rendered frame",
						sec.ID, stepIdx+1, step.Caption, want,
					)
				}
			}

			csec.Steps = append(csec.Steps, CapturedStep{
				SectionID: sec.ID,
				Index:     globalIndex,
				Caption:   step.Caption,
				Command:   step.Command,
				Keys:      keys,
				KeyBadge:  badge,
				ANSI:      ansi,
				Plain:     plain,
				PauseMs:   step.PauseMs,
				Captured:  step.ShouldCapture(),
			})
		}
		res.Sections = append(res.Sections, csec)
	}

	return res, nil
}
