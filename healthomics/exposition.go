package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

type label struct{ name, value string }

type sample struct {
	labels []label
	value  float64
}

type family struct {
	name, help, kind string
	samples          []sample
}

func (f *family) add(value float64, labels ...label) {
	f.samples = append(f.samples, sample{labels: labels, value: value})
}

var labelValueEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

// writeFamilies renders families in the Prometheus text format (0.0.4). Labels are sorted
// by name and empty label values are omitted, which Prometheus treats as an absent label.
func writeFamilies(w io.Writer, families []*family) {
	for _, f := range families {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", f.name, f.help, f.name, f.kind)
		for _, s := range f.samples {
			labels := make([]label, 0, len(s.labels))
			for _, l := range s.labels {
				if l.value != "" {
					labels = append(labels, l)
				}
			}
			sort.Slice(labels, func(i, j int) bool { return labels[i].name < labels[j].name })
			io.WriteString(w, f.name)
			if len(labels) > 0 {
				parts := make([]string, len(labels))
				for i, l := range labels {
					parts[i] = l.name + `="` + labelValueEscaper.Replace(l.value) + `"`
				}
				io.WriteString(w, "{"+strings.Join(parts, ",")+"}")
			}
			io.WriteString(w, " "+strconv.FormatFloat(s.value, 'g', -1, 64)+"\n")
		}
	}
}
