package printer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/alecthomas/template"
	"github.com/tab1293/ghz"
)

const (
	barChar = "∎"
)

// ReportPrinter is used for printing the report
type ReportPrinter struct {
	Out    io.Writer
	Report *ghz.Report
}

// Print the report using the given format
// If format is "csv" detailed listing is printer in csv format.
// Otherwise the summary of results is printed.
func (rp *ReportPrinter) Print(format string) {
	switch format {
	case "", "csv":
		outputTmpl := defaultTmpl
		if format == "csv" {
			outputTmpl = csvTmpl
		}
		buf := &bytes.Buffer{}
		templ := template.Must(template.New("tmpl").Funcs(tmplFuncMap).Parse(outputTmpl))
		if err := templ.Execute(buf, *rp.Report); err != nil {
			log.Println("error:", err.Error())
			return
		}

		rp.printf(buf.String())

		rp.printf("\n")
	case "json", "pretty":
		rep, err := json.Marshal(*rp.Report)
		if err != nil {
			log.Println("error:", err.Error())
			return
		}

		if format == "pretty" {
			var out bytes.Buffer
			err = json.Indent(&out, rep, "", "  ")
			if err != nil {
				log.Println("error:", err.Error())
				return
			}
			rep = out.Bytes()
		}

		rp.printf(string(rep))
	case "html":
		buf := &bytes.Buffer{}
		templ := template.Must(template.New("tmpl").Funcs(tmplFuncMap).Parse(htmlTmpl))
		if err := templ.Execute(buf, *rp.Report); err != nil {
			log.Println("error:", err.Error())
			return
		}

		rp.printf(buf.String())
	}
}

func (rp *ReportPrinter) printf(s string, v ...interface{}) {
	fmt.Fprintf(rp.Out, s, v...)
}

var tmplFuncMap = template.FuncMap{
	"formatMilli":   formatMilli,
	"formatSeconds": formatSeconds,
	"histogram":     histogram,
	"jsonify":       jsonify,
	"formatMark":    formatMarkMs,
	"formatPercent": formatPercent,
}

func jsonify(v interface{}, pretty bool) string {
	d, _ := json.Marshal(v)
	if !pretty {
		return string(d)
	}

	var out bytes.Buffer
	err := json.Indent(&out, d, "", "  ")
	if err != nil {
		return string(d)
	}

	return string(out.Bytes())
}

func formatMilli(duration float64) string {
	return fmt.Sprintf("%4.2f", duration*1000)
}

func formatSeconds(duration float64) string {
	return fmt.Sprintf("%4.2f", duration)
}

func formatPercent(num int, total uint64) string {
	p := float64(num) / float64(total)
	return fmt.Sprintf("%.2f", p*100)
}

func histogram(buckets []ghz.Bucket) string {
	max := 0
	for _, b := range buckets {
		if v := b.Count; v > max {
			max = v
		}
	}
	res := new(bytes.Buffer)
	for i := 0; i < len(buckets); i++ {
		// Normalize bar lengths.
		var barLen int
		if max > 0 {
			barLen = (buckets[i].Count*40 + max/2) / max
		}
		res.WriteString(fmt.Sprintf("  %4.3f [%v]\t|%v\n", buckets[i].Mark*1000, buckets[i].Count, strings.Repeat(barChar, barLen)))
	}
	return res.String()
}

func formatMarkMs(m float64) string {
	return fmt.Sprintf("'%4.3f ms'", m*1000)
}

var (
	defaultTmpl = `
Summary:
  Count:	{{ .Count }}
  Total:	{{ formatMilli .Total.Seconds }} ms
  Slowest:	{{ formatMilli .Slowest.Seconds }} ms
  Fastest:	{{ formatMilli .Fastest.Seconds }} ms
  Average:	{{ formatMilli .Average.Seconds }} ms
  Requests/sec:	{{ formatSeconds .Rps }}

Response time histogram:
{{ histogram .Histogram }}
Latency distribution:{{ range .LatencyDistribution }}
  {{ .Percentage }}%% in {{ formatMilli .Latency.Seconds }} ms{{ end }}
Status code distribution:{{ range $code, $num := .StatusCodeDist }}
  [{{ $code }}]	{{ $num }} responses{{ end }}
{{ if gt (len .ErrorDist) 0 }}Error distribution:{{ range $err, $num := .ErrorDist }}
  [{{ $num }}]	{{ $err }}{{ end }}{{ end }}
`

	csvTmpl = `
duration (ms),status,error{{ range $i, $v := .Details }}
{{ formatMilli .Latency.Seconds }},{{ .Status }},{{ .Error }}{{ end }}
`

	htmlTmpl = `
<html>
  <head>
    <meta charset="utf-8">
    <meta http-equiv="X-UA-Compatible" content="IE=edge">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Results</title>
    <script src="https://d3js.org/d3.v5.min.js"></script>
		<script src="https://cdn.jsdelivr.net/npm/papaparse@4.5.0/papaparse.min.js"></script>
    <script src="https://cdn.jsdelivr.net/npm/britecharts@2/dist/bundled/britecharts.min.js"></script>

    <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/britecharts/dist/css/britecharts.min.css" type="text/css" /></head>
    <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/bulma/0.7.1/css/bulma.min.css" />

  </head>

	<body>

		<section class="section">

		<div class="container">
      <nav class="breadcrumb has-bullet-separator" aria-label="breadcrumbs">
        <ul>
          <li>
            <a href="#summary">
              <span class="icon is-small">
                <i class="fas fa-clipboard-list" aria-hidden="true"></i>
              </span>
              <span>Summary</span>
            </a>
          </li>
          <li>
            <a href="#histogram">
              <span class="icon is-small">
                <i class="fas fa-chart-bar" aria-hidden="true"></i>
              </span>
              <span>Histogram</span>
            </a>
          </li>
          <li>
            <a href="#latency">
              <span class="icon is-small">
                <i class="far fa-clock" aria-hidden="true"></i>
              </span>
              <span>Latency Distribution</span>
            </a>
          </li>
          <li>
            <a href="#status">
              <span class="icon is-small">
                <i class="far fa-check-square" aria-hidden="true"></i>
              </span>
              <span>Status Distribution</span>
            </a>
					</li>
					{{ if gt (len .ErrorDist) 0 }}
          <li>
            <a href="#errors">
              <span class="icon is-small">
                <i class="fas fa-exclamation-circle" aria-hidden="true"></i>
              </span>
              <span>Errors</span>
            </a>
					</li>
					{{ end }}
          <li>
            <a href="#data">
              <span class="icon is-small">
                <i class="far fa-file-alt" aria-hidden="true"></i>
              </span>
              <span>Data</span>
            </a>
          </li>
        </ul>
      </nav>
      <hr />
    </div>

	  <div class="container">
			<div class="columns">
				<div class="column is-narrow">
					<div class="content">
						<a name="summary">
							<h3>Summary</h3>
						</a>
						<table class="table">
							<tbody>
								<tr>
								<th>Count</th>
								<td>{{ .Count }}</td>
								</tr>
								<tr>
									<th>Total</th>
									<td>{{ formatMilli .Total.Seconds }} ms</td>
								</tr>
								<tr>
									<th>Slowest</th>
								<td>{{ formatMilli .Slowest.Seconds }} ms</td>
								</tr>
								<tr>
									<th>Fastest</th>
									<td>{{ formatMilli .Fastest.Seconds }} ms</td>
								</tr>
								<tr>
									<th>Average</th>
									<td>{{ formatMilli .Average.Seconds }} ms</td>
								</tr>
								<tr>
									<th>Requests / sec</th>
									<td>{{ formatSeconds .Rps }}</td>
								</tr>
							</tbody>
						</table>
					</div>
				</div>
				<div class="column">
					<div class="content">
						<span class="title is-5">
							<strong>Options</strong>
						</span>
						<article class="message">
  						<div class="message-body">
								<pre style="background-color: transparent;">{{ jsonify .Options true }}</pre>
							</div>
						</article>
					</div>
				</div>
			</div>
	  </div>

	  <br />
		<div class="container">
			<div class="content">
				<a name="historam">
					<h3>Historam</h3>
				</a>
				<p>
					<div class="js-bar-container"></div>
				</p>
			</div>
	  </div>

	  <br />
		<div class="container">
			<div class="content">
				<a name="latency">
					<h3>Latency distribution</h3>
				</a>
				<table class="table is-fullwidth">
					<thead>
						<tr>
							{{ range .LatencyDistribution }}
								<th>{{ .Percentage }} %%</th>
							{{ end }}
						</tr>
					</thead>
					<tbody>
						<tr>
							{{ range .LatencyDistribution }}
								<td>{{ formatMilli .Latency.Seconds }} ms</td>
							{{ end }}
						</tr>
					</tbody>
				</table>
			</div>
		</div>

		<br />
		<div class="container">
			<div class="columns">
				<div class="column is-narrow">
					<div class="content">
						<a name="status">
							<h3>Status distribution</h3>
						</a>
						<table class="table is-hoverable">
							<thead>
								<tr>
									<th>Status</th>
									<th>Count</th>
									<th>%% of Total</th>
								</tr>
							</thead>
							<tbody>
							  {{ range $code, $num := .StatusCodeDist }}
									<tr>
									  <td>{{ $code }}</td>
										<td>{{ $num }}</td>
										<td>{{ formatPercent $num $.Count }} %%</td>
									</tr>
									{{ end }}
								</tbody>
							</table>
						</div>
					</div>
				</div>
			</div>

			{{ if gt (len .ErrorDist) 0 }}

				<br />
				<div class="container">
					<div class="columns">
						<div class="column is-narrow">
							<div class="content">
								<a name="errors">
									<h3>Errors</h3>
								</a>
								<table class="table is-hoverable">
									<thead>
										<tr>
											<th>Error</th>
											<th>Count</th>
											<th>%% of Total</th>
										</tr>
									</thead>
									<tbody>
										{{ range $err, $num := .ErrorDist }}
											<tr>
												<td>{{ $err }}</td>
												<td>{{ $num }}</td>
												<td>{{ formatPercent $num $.Count }} %%</td>
											</tr>
											{{ end }}
										</tbody>
									</table>
								</div>
							</div>
						</div>
					</div>

			{{ end }}

			<br />
      <div class="container">
        <div class="columns">
          <div class="column is-narrow">
            <div class="content">
              <a name="data">
                <h3>Data</h3>
              </a>

              <a class="button" id="dlJSON">JSON</a>
              <a class="button" id="dlCSV">CSV</a>
            </div>
          </div>
        </div>
			</div>

			<div class="container">
        <hr />
        <div class="content has-text-centered">
          <p>
            Generated by <strong>ghz</strong>
          </p>
          <a href="https://github.com/tab1293/ghz"><i class="icon is-medium fab fa-github"></i></a>
        </div>
      </div>

		</section>

  </body>

  <script>

	const count = {{ .Count }};

	const rawData = {{ jsonify .Details false }};

	const data = [
		{{ range .Histogram }}
			{ name: {{ formatMark .Mark }}, value: {{ .Count }} },
		{{ end }}
	];

	function createHorizontalBarChart() {
		let barChart = britecharts.bar(),
			tooltip = britecharts.miniTooltip(),
			barContainer = d3.select('.js-bar-container'),
			containerWidth = barContainer.node() ? barContainer.node().getBoundingClientRect().width : false,
			tooltipContainer,
			dataset;

		tooltip.numberFormat('')
		tooltip.valueFormatter(function(v) {
			var percent = v / count * 100;
			return v + ' ' + '(' + Number.parseFloat(percent).toFixed(1) + ' %%)';
		})

		if (containerWidth) {
			dataset = data;
			barChart
				.isHorizontal(true)
				.isAnimated(true)
				.margin({
					left: 100,
					right: 20,
					top: 20,
					bottom: 20
				})
				.colorSchema(britecharts.colors.colorSchemas.teal)
				.width(containerWidth)
				.yAxisPaddingBetweenChart(20)
				.height(400)
				// .hasPercentage(true)
				.enableLabels(true)
				.labelsNumberFormat('')
				.percentageAxisToMaxRatio(1.3)
				.on('customMouseOver', tooltip.show)
				.on('customMouseMove', tooltip.update)
				.on('customMouseOut', tooltip.hide);

			barChart.orderingFunction(function(a, b) {
				var nA = a.name.replace(/ms/gi, '');
				var nB = b.name.replace(/ms/gi, '');

				var vA = Number.parseFloat(nA);
				var vB = Number.parseFloat(nB);

				return vB - vA;
			})

			barContainer.datum(dataset).call(barChart);

			tooltipContainer = d3.select('.js-bar-container .bar-chart .metadata-group');
			tooltipContainer.datum([]).call(tooltip);
		}
	}

	function setJSONDownloadLink () {
		var filename = "data.json";
		var btn = document.getElementById('dlJSON');
		var jsonData = JSON.stringify(rawData)
		var blob = new Blob([jsonData], { type: 'text/json;charset=utf-8;' });
		var url = URL.createObjectURL(blob);
		btn.setAttribute("href", url);
		btn.setAttribute("download", filename);
	}

	function setCSVDownloadLink () {
		var filename = "data.csv";
		var btn = document.getElementById('dlCSV');
		var csv = Papa.unparse(rawData)
		var blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
		var url = URL.createObjectURL(blob);
		btn.setAttribute("href", url);
		btn.setAttribute("download", filename);
	}

	createHorizontalBarChart();

	setJSONDownloadLink();

	setCSVDownloadLink();

	</script>

	<script defer src="https://use.fontawesome.com/releases/v5.1.0/js/all.js"></script>

</html>
`
)
