package sysstat

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
)

// readTicks reads the aggregate "cpu" line of /proc/stat: busy is
// everything but idle (iowait counts as busy); guest time is left out, the
// kernel already counts it in user.
func readTicks() (ticks, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return ticks{}, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		if len(fields) < 4 {
			break
		}
		var t ticks
		for i, field := range fields[:min(len(fields), 8)] {
			v, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return ticks{}, err
			}
			t.total += v
			if i == 3 {
				t.idle = v
			}
		}
		return t, nil
	}
	return ticks{}, errors.New("/proc/stat has no cpu line")
}

// readMemory reads /proc/meminfo: used is MemTotal less MemAvailable.
func readMemory() (Memory, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return Memory{}, err
	}
	var total, avail uint64
	for line := range strings.Lines(string(data)) {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			total = v * 1024
		case "MemAvailable:":
			avail = v * 1024
		}
	}
	if total == 0 {
		return Memory{}, errors.New("/proc/meminfo has no MemTotal")
	}
	return Memory{Used: total - min(avail, total), Total: total}, nil
}
