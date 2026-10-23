package main

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"ochazuke"
)

func plain(text string) string {
	return strings.Map(func(ch rune) rune {
		if unicode.IsControl(ch) && ch != '\n' && ch != '\t' {
			return -1
		}
		return ch
	}, ansi.Strip(text))
}

func registerPages(m *ochazuke.Model, specs []PageConfig) error {
	for _, spec := range specs {
		if spec.Renderer != "workspace" && spec.Renderer != "text" {
			return fmt.Errorf("page %q: unknown renderer %q", spec.ID, spec.Renderer)
		}
		for _, id := range spec.Inputs {
			path := m.Runtime.InputPaths[id]
			if len(path) < 2 || path[1] != spec.ID {
				return fmt.Errorf("page %q: input %q is unknown or belongs to another page", spec.ID, id)
			}
		}
		if spec.Context.InputID != "" && !slices.Contains(spec.Inputs, spec.Context.InputID) {
			return fmt.Errorf("page %q: focused input must appear in inputs", spec.ID)
		}
		page := &ochazuke.Page{ID: spec.ID, Title: spec.Title, Frame: &ochazuke.Frame{ID: spec.ID, Context: spec.Context, State: map[string]any{}}}
		page.Render = func(r *ochazuke.Runtime, width, height int) string {
			styles := ochazuke.NewStyles(*m.Appearance)
			var content strings.Builder
			content.WriteString(styles.Title.Render(plain(spec.Title)) + "\n" + plain(spec.Text))
			if spec.Renderer == "text" {
				return content.String()
			}
			frame := r.Pages[spec.ID].Frame
			for _, id := range spec.Inputs {
				input := r.Inputs[id]
				marker := "  "
				if frame.Context.InputID == id {
					marker = "› "
				}
				label := marker + id
				if input.Kind == "path" || input.Kind == "paths" {
					label += " · " + input.Kind + " / " + input.PathFilter
				}
				bodyHeight := 1
				if input.Multiline {
					bodyHeight = max(2, min(6, height/3))
				}
				content.WriteString("\n\n" + styles.Subtitle.Render(label) + "\n" + ochazuke.InputView(input, width, bodyHeight))
			}
			for _, key := range []string{"lastAction", "summary", "greeting", "inspection"} {
				if value, ok := frame.State[key]; ok {
					fmt.Fprintf(&content, "\n\n%s", styles.Accent.Render(plain(fmt.Sprint(value))))
				}
			}
			return content.String()
		}
		if err := m.Runtime.RegisterPage(page); err != nil {
			return fmt.Errorf("page %q: %w", spec.ID, err)
		}
	}
	return nil
}

func registerModals(m *ochazuke.Model, specs []ModalConfig) error {
	for _, spec := range specs {
		if strings.TrimSpace(spec.ID) == "" || m.ModalViews["modal:"+spec.ID].Render != nil {
			return fmt.Errorf("modal %q is empty or duplicate", spec.ID)
		}
		if spec.Renderer != "text" && spec.Renderer != "confirm" {
			return fmt.Errorf("modal %q: unknown renderer %q", spec.ID, spec.Renderer)
		}
		if spec.Input != "" && m.Runtime.Inputs[spec.Input] == nil {
			return fmt.Errorf("modal %q: unknown input %q", spec.ID, spec.Input)
		}
		if spec.Renderer == "confirm" && spec.Input == "" {
			return fmt.Errorf("modal %q: confirm renderer requires an input", spec.ID)
		}
		m.ModalViews["modal:"+spec.ID] = ochazuke.ModalView{Title: spec.Title, Render: func(r *ochazuke.Runtime, frame *ochazuke.Frame, w, h int) string {
			body := plain(spec.Text)
			if spec.Renderer == "confirm" {
				body += "\n\n" + ochazuke.InputView(r.Inputs[frame.Context.InputID], w, max(1, h-3))
			}
			return body
		}}
	}
	return nil
}
