package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/regexp"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/promqltest"
	"github.com/prometheus/prometheus/storage"
	prom_httputil "github.com/prometheus/prometheus/util/httputil"
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

type series struct {
	Series string `yaml:"series"`
	Values string `yaml:"values"`
}

type engine struct {
	suite *promqltest.LazyLoader
}

func setup() *engine {
	// TODO: load from file
	var inputSeries []series = []series{
		{
			Series: `container_fs_writes_total{prometheus="monitoring/kube-prometheus",service="kubelet"}`,
			Values: "935 935 935 935 935 935 935 935 935 935 935",
		},
	}

	seriesLoadingString := fmt.Sprintf("load %v\n", shortDuration(model.Duration(1*time.Minute)))
	for _, is := range inputSeries {
		seriesLoadingString += fmt.Sprintf("  %v %v\n", is.Series, is.Values)
	}

	// Setup testing suite.
	suite, err := promqltest.NewLazyLoader(seriesLoadingString, promqltest.LazyLoaderOpts{
		EnableAtModifier:     true,
		EnableNegativeOffset: true,
	})
	if err != nil {
		panic(err)
	}
	/* 	defer func() {
		err := suite.Close()
		if err != nil {
			panic(err)
		}
	}() */
	suite.SubqueryInterval = 1 * time.Minute

	return &engine{suite: suite}
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

var e *engine = setup()

func Handle(w http.ResponseWriter, r *http.Request) {
	regex, err := regexp.Compile("^(?:.*)$")
	if err != nil {
		panic(err)
	}
	prom_httputil.SetCORS(w, regex, r)

	samples, err := e.exec(r.FormValue("query"))
	if err != nil {
		http.Error(w, fmt.Sprintf("error while executin query: %v", err), http.StatusInternalServerError)
	}

	resp := Response{
		Status: "success",
		Data: Result{
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

type Result struct {
	ResultType string      `json:"resultType,omitempty"`
	Result     interface{} `json:"result,omitempty"`
}

type Response struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
}
