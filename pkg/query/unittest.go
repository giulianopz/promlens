package query

/*
	Prometheus unit tests file
	see: https://prometheus.io/docs/prometheus/latest/configuration/unit_testing_rules
*/

type inputSeries struct {
	Series string `yaml:"series"`
	Values string `yaml:"values"`
}

type expLabels struct {
	Severity string `yaml:"severity"`
	Instance string `yaml:"instance"`
	Job      string `yaml:"job"`
}

type expAnnotations struct {
	Summary     string `yaml:"summary"`
	Description string `yaml:"description"`
}

type expAlerts struct {
	ExpLabels      expLabels      `yaml:"exp_labels"`
	ExpAnnotations expAnnotations `yaml:"exp_annotations"`
}

type alertRuleTest struct {
	EvalTime  string      `yaml:"eval_time"`
	Alertname string      `yaml:"alertname"`
	ExpAlerts []expAlerts `yaml:"exp_alerts"`
}

type expSample struct {
	Labels string  `yaml:"labels"`
	Value  float64 `yaml:"value"`
}

type promqlExprTest struct {
	Expr       string      `yaml:"expr"`
	Name       string      `yaml:"name"`
	EvalTime   string      `yaml:"eval_time"`
	ExpSamples []expSample `yaml:"exp_samples"`
}

type unitTest struct {
	Interval        string           `yaml:"interval"`
	InputSeries     []inputSeries    `yaml:"input_series"`
	AlertRuleTests  []alertRuleTest  `yaml:"alert_rule_test"`
	PromqlExprTests []promqlExprTest `yaml:"promql_expr_test"`
}

type promUnitTestsFile struct {
	RuleFiles          []string   `yaml:"rule_files"`
	EvaluationInterval string     `yaml:"evaluation_interval"`
	Tests              []unitTest `yaml:"tests"`
}
