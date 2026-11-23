package chart

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"strings"

	plotfont "gonum.org/v1/plot/font"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/text"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
)

type sankeyData struct {
	Datasets []sankeyDataset `json:"datasets"`
}

type sankeyDataset struct {
	Data   []sankeyFlow      `json:"data"`
	Color  map[string]string `json:"color"`
	Labels map[string]string `json:"labels"`
	Column map[string]int    `json:"column"`
}

type sankeyFlow struct {
	From  string  `json:"from"`
	To    string  `json:"to"`
	Flow  float64 `json:"flow"`
	Count int     `json:"count"`
}

func renderSankey(req Request) ([]byte, error) {
	var d sankeyData
	if err := json.Unmarshal(req.Data, &d); err != nil {
		return nil, fmt.Errorf("invalid sankey data: %w", err)
	}
	if len(d.Datasets) == 0 {
		return nil, fmt.Errorf("sankey data has no datasets")
	}
	ds := d.Datasets[0]

	entries := make([]flowEntry, 0, len(ds.Data))
	for _, f := range ds.Data {
		value := float64(f.Count)
		if value == 0 {
			value = f.Flow
		}
		if value <= 0 {
			continue
		}
		srcCol, srcOk := ds.Column[f.From]
		dstCol, dstOk := ds.Column[f.To]
		if !srcOk || !dstOk {
			continue
		}
		entries = append(entries, flowEntry{
			flow: plotter.Flow{
				SourceLabel:      resolveLabel(ds.Labels, f.From),
				ReceptorLabel:    resolveLabel(ds.Labels, f.To),
				SourceCategory:   srcCol,
				ReceptorCategory: dstCol,
				Value:            value,
				Group:            f.From,
			},
			percent: f.Flow,
		})
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("sankey data has no valid flows")
	}

	flows := make([]plotter.Flow, len(entries))
	for i, e := range entries {
		flows[i] = e.flow
	}

	s, err := plotter.NewSankey(flows...)
	if err != nil {
		return nil, fmt.Errorf("creating sankey: %w", err)
	}

	sansFont := plotfont.Font{
		Typeface: "Liberation",
		Variant:  "Sans",
		Size:     plotfont.Points(12),
	}

	noBorder := draw.LineStyle{Width: 0}
	s.LineStyle = noBorder
	s.StockBarWidth = vg.Points(14)

	s.TextStyle = text.Style{
		Font:     sansFont,
		Rotation: math.Pi / 2,
		XAlign:   text.XCenter,
		YAlign:   text.YBottom,
		Handler:  text.Plain{Fonts: plotfont.DefaultCache},
		Color:    color.Black,
	}

	colorMap := buildColorMap(ds.Color)

	s.FlowStyle = func(group string) (color.Color, draw.LineStyle) {
		if c, ok := colorMap[group]; ok {
			return withAlpha(c, 140), noBorder
		}
		return withAlpha(s.Color, 140), noBorder
	}

	s.StockStyle = func(label string, category int) (string, text.Style, vg.Length, vg.Length, color.Color, draw.LineStyle) {
		ts := s.TextStyle
		c := s.Color
		for key, col := range colorMap {
			if resolveLabel(ds.Labels, key) == label {
				c = col
				break
			}
		}
		return label, ts, s.StockBarWidth/4 + vg.Points(1), 0, c, noBorder
	}

	p := plot.New()
	p.Title.Text = stripHTML(req.Label)
	p.Title.TextStyle.Font = sansFont
	p.Title.Padding = vg.Points(10)
	p.Add(s)
	p.HideAxes()

	labels, err := buildFlowLabels(s, entries, sansFont)
	if err == nil && labels != nil {
		p.Add(labels)
	}

	wt, err := p.WriterTo(vg.Length(defaultWidth), vg.Length(defaultHeight), "png")
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if _, err := wt.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type flowEntry struct {
	flow    plotter.Flow
	percent float64
}

func buildFlowLabels(s *plotter.Sankey, entries []flowEntry, fnt plotfont.Font) (*plotter.Labels, error) {
	type stockKey struct {
		label    string
		category int
	}
	sourceOffsets := make(map[stockKey]float64)

	pts := make(plotter.XYs, 0, len(entries))
	lbls := make([]string, 0, len(entries))

	for _, e := range entries {
		f := e.flow
		sk := stockKey{label: f.SourceLabel, category: f.SourceCategory}
		sMin, _, err := s.StockRange(f.SourceLabel, f.SourceCategory)
		if err != nil {
			continue
		}

		flowStart := sMin + sourceOffsets[sk]
		flowCenter := flowStart + f.Value/2
		sourceOffsets[sk] += f.Value

		xPos := float64(f.SourceCategory) + 0.5

		pts = append(pts, plotter.XY{X: xPos, Y: flowCenter})
		lbls = append(lbls, fmt.Sprintf("%.1f%%", e.percent))
	}

	if len(pts) == 0 {
		return nil, nil
	}

	labelData := plotter.XYLabels{
		XYs:    pts,
		Labels: lbls,
	}
	l, err := plotter.NewLabels(labelData)
	if err != nil {
		return nil, err
	}

	labelFont := fnt
	labelFont.Size = plotfont.Points(10)
	labelStyle := text.Style{
		Font:    labelFont,
		XAlign:  text.XCenter,
		Handler: text.Plain{Fonts: plotfont.DefaultCache},
		Color:   color.RGBA{R: 40, G: 40, B: 40, A: 255},
	}
	for i := range l.TextStyle {
		l.TextStyle[i] = labelStyle
	}

	return l, nil
}

func resolveLabel(labels map[string]string, key string) string {
	if labels != nil {
		if v, ok := labels[key]; ok {
			return stripHTML(v)
		}
	}
	return key
}

func buildColorMap(colors map[string]string) map[string]color.Color {
	if len(colors) == 0 {
		return nil
	}
	m := make(map[string]color.Color, len(colors))
	for key, hex := range colors {
		if c, ok := parseHexColor(hex); ok {
			m[key] = c
		}
	}
	return m
}

func withAlpha(c color.Color, a uint8) color.Color {
	r, g, b, _ := c.RGBA()
	return color.NRGBA{R: to8(r), G: to8(g), B: to8(b), A: a}
}

func to8(v uint32) uint8 {
	v >>= 8
	if v > 0xff {
		return 0xff
	}
	return uint8(v)
}

func parseHexColor(s string) (color.Color, bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return nil, false
	}
	var rgb [3]uint8
	for i := range 3 {
		v, err := fmt.Sscanf(s[i*2:i*2+2], "%02x", &rgb[i])
		if err != nil || v != 1 {
			return nil, false
		}
	}
	return color.RGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: 255}, true
}
