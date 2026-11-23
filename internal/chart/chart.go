package chart

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"

	"github.com/go-analyze/charts"
)

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

type Request struct {
	Type    string          `json:"type"`
	Label   string          `json:"label"`
	Subtype string          `json:"subtype"`
	Data    json.RawMessage `json:"data"`
}

type chartData struct {
	Labels   []string       `json:"labels"`
	Datasets []chartDataset `json:"datasets"`
}

type chartDataset struct {
	Label string    `json:"label"`
	Data  []float64 `json:"data"`
}

type scatterData struct {
	Datasets []scatterDataset `json:"datasets"`
}

type scatterDataset struct {
	Label string         `json:"label"`
	Data  []scatterPoint `json:"data"`
}

type scatterPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type knotsData struct {
	Stages []knotsStage `json:"stages"`
}

type knotsStage struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

const (
	defaultWidth  = 600
	defaultHeight = 400
)

func Render(req Request) ([]byte, error) {
	p := charts.NewPainter(charts.PainterOptions{
		Width:  defaultWidth,
		Height: defaultHeight,
	})

	title := charts.TitleOption{Text: stripHTML(req.Label)}

	switch req.Type {
	case "bar":
		return renderBar(p, req, title)
	case "line":
		return renderLine(p, req, title)
	case "doughnut":
		p = charts.NewPainter(charts.PainterOptions{
			Width:  900,
			Height: defaultHeight,
		})
		return renderDoughnut(p, req, title)
	case "scatter":
		return renderScatter(p, req, title)
	case "knots":
		return renderKnots(p, req, title)
	case "sankey":
		return renderSankey(req)
	default:
		return nil, fmt.Errorf("unsupported chart type: %q", req.Type)
	}
}

func renderBar(p *charts.Painter, req Request, title charts.TitleOption) ([]byte, error) {
	var d chartData
	if err := json.Unmarshal(req.Data, &d); err != nil {
		return nil, fmt.Errorf("invalid bar data: %w", err)
	}
	opt := charts.NewBarChartOptionWithData(datasetValues(d.Datasets))
	opt.Title = title
	opt.Legend = hiddenLegend(datasetNames(d.Datasets))
	opt.CategoryAxis.Labels = stripHTMLSlice(d.Labels)
	if err := p.BarChart(opt); err != nil {
		return nil, err
	}
	return p.Bytes()
}

func renderLine(p *charts.Painter, req Request, title charts.TitleOption) ([]byte, error) {
	var d chartData
	if err := json.Unmarshal(req.Data, &d); err != nil {
		return nil, fmt.Errorf("invalid line data: %w", err)
	}
	opt := charts.NewLineChartOptionWithData(datasetValues(d.Datasets))
	opt.Title = title
	opt.Legend = hiddenLegend(datasetNames(d.Datasets))
	opt.XAxis.Labels = stripHTMLSlice(d.Labels)
	if err := p.LineChart(opt); err != nil {
		return nil, err
	}
	return p.Bytes()
}

func renderDoughnut(p *charts.Painter, req Request, title charts.TitleOption) ([]byte, error) {
	var d chartData
	if err := json.Unmarshal(req.Data, &d); err != nil {
		return nil, fmt.Errorf("invalid doughnut data: %w", err)
	}
	var values []float64
	if len(d.Datasets) > 0 {
		values = d.Datasets[0].Data
	}
	opt := charts.NewDoughnutChartOptionWithData(values)
	opt.Title = title
	opt.Legend = hiddenLegend(stripHTMLSlice(d.Labels))
	if err := p.DoughnutChart(opt); err != nil {
		return nil, err
	}
	return p.Bytes()
}

func renderScatter(p *charts.Painter, req Request, title charts.TitleOption) ([]byte, error) {
	var d scatterData
	if err := json.Unmarshal(req.Data, &d); err != nil {
		return nil, fmt.Errorf("invalid scatter data: %w", err)
	}
	values := make([][]float64, len(d.Datasets))
	var labels []string
	for i, ds := range d.Datasets {
		sorted := make([]scatterPoint, len(ds.Data))
		copy(sorted, ds.Data)
		sort.Slice(sorted, func(a, b int) bool { return sorted[a].X < sorted[b].X })
		yVals := make([]float64, len(sorted))
		if i == 0 {
			labels = make([]string, len(sorted))
		}
		for j, pt := range sorted {
			yVals[j] = pt.Y
			if i == 0 {
				labels[j] = strconv.FormatFloat(pt.X, 'f', -1, 64)
			}
		}
		values[i] = yVals
	}
	names := make([]string, len(d.Datasets))
	for i, ds := range d.Datasets {
		names[i] = stripHTML(ds.Label)
	}
	opt := charts.NewScatterChartOptionWithData(values)
	opt.Title = title
	opt.Legend = hiddenLegend(names)
	opt.XAxis.Labels = labels
	if err := p.ScatterChart(opt); err != nil {
		return nil, err
	}
	return p.Bytes()
}

func renderKnots(p *charts.Painter, req Request, title charts.TitleOption) ([]byte, error) {
	var d knotsData
	if err := json.Unmarshal(req.Data, &d); err != nil {
		return nil, fmt.Errorf("invalid knots data: %w", err)
	}
	labels := make([]string, len(d.Stages))
	values := make([]float64, len(d.Stages))
	for i, s := range d.Stages {
		labels[i] = stripHTML(s.Label)
		values[i] = s.Value
	}
	opt := charts.NewLineChartOptionWithData([][]float64{values})
	opt.Title = title
	opt.Legend.Show = charts.Ptr(false)
	opt.XAxis.Labels = labels
	opt.StrokeSmoothingTension = 0.5

	if err := p.LineChart(opt); err != nil {
		return nil, err
	}
	return p.Bytes()
}

func datasetValues(datasets []chartDataset) [][]float64 {
	values := make([][]float64, len(datasets))
	for i, ds := range datasets {
		values[i] = ds.Data
	}
	return values
}

func hiddenLegend(names []string) charts.LegendOption {
	return charts.LegendOption{
		Show:        charts.Ptr(false),
		SeriesNames: names,
	}
}

func datasetNames(datasets []chartDataset) []string {
	names := make([]string, len(datasets))
	for i, ds := range datasets {
		names[i] = stripHTML(ds.Label)
	}
	return names
}

func stripHTML(s string) string {
	return htmlTagRe.ReplaceAllString(s, "")
}

func stripHTMLSlice(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = stripHTML(s)
	}
	return out
}
