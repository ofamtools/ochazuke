package setup

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

func (s *Setup) env() map[string]string {
	env := map[string]string{}
	for _, e := range os.Environ() {
		k, v, _ := strings.Cut(e, "=")
		if !s.homeOverride || k == "PATH" || k == "LANG" || strings.HasPrefix(k, "LC_") || k == "SystemRoot" || k == "SYSTEMROOT" || k == "PATHEXT" {
			env[k] = v
		}
	}
	if s.homeOverride {
		// A dry-run home never consults host-specific harness directories.
		// Item path expansion also ignores inherited variables in this mode.
		env["HOME"] = s.options.Home
		for k, v := range map[string]string{"XDG_CONFIG_HOME": ".config", "XDG_STATE_HOME": ".local/state", "XDG_DATA_HOME": ".local/share", "XDG_CACHE_HOME": ".cache"} {
			env[k] = filepath.Join(s.options.Home, v)
		}
	}
	for k, v := range s.options.Env {
		env[k] = v
	}
	return env
}

func (s *Setup) environment() []string {
	m := s.env()
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
}

// expand implements ~, $ENV and nested ${ENV:-default}; explicit Home ignores
// inherited path variables so a fixture cannot resolve into the owner's home.
func (s *Setup) expand(path string) (string, error) {
	var expand func(string, int) (string, error)
	expand = func(in string, depth int) (string, error) {
		if depth > 16 {
			return "", fmt.Errorf("path expansion is too deeply nested")
		}
		var out strings.Builder
		for len(in) > 0 {
			at := strings.IndexByte(in, '$')
			if at < 0 {
				out.WriteString(in)
				break
			}
			out.WriteString(in[:at])
			in = in[at+1:]
			key, fallback := "", ""
			if strings.HasPrefix(in, "{") {
				level, end := 1, 1
				for ; end < len(in) && level > 0; end++ {
					if in[end] == '{' {
						level++
					}
					if in[end] == '}' {
						level--
					}
				}
				if level != 0 {
					return "", fmt.Errorf("unclosed environment expansion")
				}
				key, fallback, _ = strings.Cut(in[1:end-1], ":-")
				in = in[end:]
			} else {
				n := 0
				for n < len(in) && ((in[n] >= 'A' && in[n] <= 'Z') || (in[n] >= 'a' && in[n] <= 'z') || (in[n] >= '0' && in[n] <= '9') || in[n] == '_') {
					n++
				}
				if n == 0 {
					out.WriteByte('$')
					continue
				}
				key, in = in[:n], in[n:]
			}
			v := s.options.Env[key]
			if key == "HOME" {
				v = s.options.Home
			} else if v == "" && !s.homeOverride {
				v = os.Getenv(key)
			}
			if v == "" {
				v = fallback
			}
			v, err := expand(v, depth+1)
			if err != nil {
				return "", err
			}
			out.WriteString(v)
		}
		v := out.String()
		if v == "~" {
			v = s.options.Home
		} else if strings.HasPrefix(v, "~/") {
			v = filepath.Join(s.options.Home, v[2:])
		}
		return v, nil
	}
	v, err := expand(path, 0)
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", fmt.Errorf("path expands to empty")
	}
	if !filepath.IsAbs(v) {
		v = filepath.Join(s.options.Home, v)
	}
	return filepath.Clean(v), nil
}

func (s *Setup) render(i Item) (Item, error) {
	data := struct{ Bin, Data, Home, Tool string }{s.options.Bin, s.options.Data, s.options.Home, s.options.Tool}
	render := func(v string) (string, error) {
		t, err := template.New(i.ID).Option("missingkey=error").Parse(v)
		if err != nil {
			return "", err
		}
		var b bytes.Buffer
		err = t.Execute(&b, data)
		return b.String(), err
	}
	for _, p := range []*string{&i.Content, &i.Advice, &i.Begin, &i.End, &i.Match.Contains} {
		v, err := render(*p)
		if err != nil {
			return i, err
		}
		*p = v
	}
	for _, p := range []*[]string{&i.Check, &i.Install, &i.Uninstall} {
		a := append([]string(nil), (*p)...)
		for n, v := range a {
			var err error
			a[n], err = render(v)
			if err != nil {
				return i, err
			}
		}
		*p = a
	}
	if i.Path != "" {
		p, err := s.expand(i.Path)
		if err != nil {
			return i, err
		}
		i.Path = p
	}
	if i.Probe != nil {
		p := *i.Probe
		i.Probe = &p
		if p.Path != "" {
			v, err := s.expand(p.Path)
			if err != nil {
				return i, err
			}
			i.Probe.Path = v
		}
	}
	if i.Kind == JSON || i.Kind == JSONArray || (i.Probe != nil && i.Content != "") {
		if !validJSON(i.Content) {
			return i, fmt.Errorf("content must render as valid JSON")
		}
	}
	return i, nil
}

func (s *Setup) lookPath(name string) (string, error) {
	if strings.ContainsRune(name, filepath.Separator) {
		info, err := os.Stat(name)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return name, nil
		}
		return "", exec.ErrNotFound
	}
	for _, dir := range filepath.SplitList(s.env()["PATH"]) {
		if !filepath.IsAbs(dir) {
			continue
		}
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}
