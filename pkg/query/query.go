package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/promql/promqltest"
	"github.com/prometheus/prometheus/storage"
)

func shortDuration(d model.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}

type engine struct {
	suite   *promqltest.LazyLoader
	values  map[string][]string
	metrics []string
}

func setup(unitTestFile string) *engine {

	bs, err := os.ReadFile(unitTestFile)
	if err != nil {
		panic(err)
	}

	var f promUnitTestsFile
	dec := yaml.NewDecoder(bytes.NewReader(bs))
	dec.KnownFields(false)
	if err := dec.Decode(&f); err != nil {
		panic(err)
	}

	values := make(map[string][]string)
	metrics := []string{}

	seriesLoadingString := fmt.Sprintf("load %v\n", shortDuration(model.Duration(1*time.Minute)))
	for _, t := range f.Tests {
		for _, is := range t.InputSeries {

			expr, err := parser.ParseExpr(is.Series)
			if err != nil {
				panic(err)
			}

			switch n := parser.Node(expr).(type) {
			case *parser.VectorSelector:
				for _, m := range n.LabelMatchers {
					values[m.Name] = append(values[m.Name], m.Value)
				}
				metrics = append(metrics, n.Name)
			}

			seriesLoadingString += fmt.Sprintf("  %v %v\n", is.Series, is.Values)
		}
	}

	suite, err := promqltest.NewLazyLoader(seriesLoadingString, promqltest.LazyLoaderOpts{
		EnableAtModifier:     true,
		EnableNegativeOffset: true,
	})
	if err != nil {
		panic(err)
	}

	suite.SubqueryInterval = 1 * time.Minute

	return &engine{suite: suite, values: values, metrics: metrics}
}

func query(ctx context.Context, qs string, t time.Time, engine *promql.Engine, qu storage.Queryable) (promql.Vector, error) {
	q, err := engine.NewInstantQuery(ctx, qu, nil, qs, t)
	if err != nil {
		return nil, err
	}
	res := q.Exec(ctx)
	if res.Err != nil {
		return nil, res.Err
	}
	switch v := res.Value.(type) {
	case promql.Vector:
		return v, nil
	case promql.Scalar:
		return promql.Vector{promql.Sample{
			T:      v.T,
			F:      v.V,
			Metric: labels.Labels{},
		}}, nil
	default:
		return nil, errors.New("rule result is not a vector or scalar")
	}
}

func (e *engine) exec(expr string) ([]*model.Sample, error) {
	var samples []*model.Sample

	mint := time.Unix(0, 0).UTC()

	e.suite.WithSamplesTill(time.Now(), func(err error) {
		if err != nil {
			panic(err)
		}

		res, err := query(e.suite.Context(), expr, mint.Add(time.Duration(1*time.Minute)),
			e.suite.QueryEngine(), e.suite.Queryable())
		if err != nil {
			panic(fmt.Errorf("    expr: %q, time: %s, err: %s", expr, time.Duration(1*time.Minute).String(), err.Error()))
		}

		for _, s := range res {
			labels := make(model.Metric, 0)
			for _, l := range s.Metric {
				labels[model.LabelName(l.Name)] = model.LabelValue(l.Value)
			}

			sample := &model.Sample{
				Value:     model.SampleValue(s.F),
				Timestamp: model.Time(s.T),
				Metric:    labels,
			}

			samples = append(samples, sample)
		}

	})

	return samples, nil
}

type queryHandler struct {
	*engine
}

func NewHanlder(unitTestFile string) *queryHandler {
	return &queryHandler{
		engine: setup(unitTestFile),
	}
}

func (h *queryHandler) Handle(w http.ResponseWriter, r *http.Request) {
	samples, err := h.exec(r.FormValue("query"))
	if err != nil {
		http.Error(w, fmt.Sprintf("error while executin query: %v", err), http.StatusInternalServerError)
	}

	resp := Response{
		Status: "success",
		Data: QueryResult{
			ResultType: "vector",
			Result:     model.Vector(samples),
		},
	}

	buf, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error marshaling AST: %v", err), http.StatusBadRequest)
		return
	}
	w.Write(buf)
}

type QueryResult struct {
	ResultType string      `json:"resultType,omitempty"`
	Result     interface{} `json:"result,omitempty"`
}

func (h *queryHandler) HandleValues(w http.ResponseWriter, r *http.Request) {
	labelName := r.PathValue("name")

	var values []string
	if labelName == "__name__" {
		values = h.metrics
	} else {
		vv, found := h.values[labelName]
		if !found {
			http.Error(w, "", http.StatusBadRequest)
		}
		values = vv
	}

	buf, err := json.Marshal(Response{
		Status: "success",
		Data:   values,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Error marshaling label values: %v", err), http.StatusBadRequest)
		return
	}
	w.Write(buf)
}

var unknown metadata = metadata{
	MetricType: "N/A",
	Help:       "N/A",
	Unit:       "N/A",
}

func (h *queryHandler) HandleMetadata(w http.ResponseWriter, r *http.Request) {
	metricName := r.FormValue("metric")

	results := make(map[string][]metadata)
	for _, n := range h.metrics {
		if metricName != "" && strings.HasPrefix(n, metricName) {
			results[n] = []metadata{unknown}
		} else {
			results[n] = []metadata{unknown}
		}
	}

	buf, err := json.Marshal(Response{
		Status: "success",
		Data:   results,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Error marshaling metric metadata: %v", err), http.StatusBadRequest)
		return
	}
	w.Write(buf)
}

type metadata struct {
	MetricType string `json:"type"`
	Help       string `json:"help"`
	Unit       string `json:"unit"`
}

type Response struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
}
