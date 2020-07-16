package main

import (
	"fmt"
	"io"
	"log"
	"text/template"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/codepipeline"
	"github.com/aws/aws-sdk-go-v2/service/codepipeline/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/dustin/go-humanize"
	"github.com/gosuri/uilive"
	"github.com/spf13/cobra"
)

const pipelineBody = `{{ .PipelineName | bold | underline}}
{{ range $index, $stage := .StageStates -}}
{{ $stage | stageDot }}  {{ $stage.StageName }}
{{- if isActionDetailRequired $stage }}
{{- range $index,$action := $stage.ActionStates }}
{{"   "}}{{ $action | actionDot }} {{$action.ActionName}} {{ if hasActionRun $action }}({{ $action | actionLastExecution}}){{ end }}
{{- end }}
{{ else }} ({{ $stage | stageLastExecution}})
{{ end -}}
{{ end -}}
`
const pipelineSummaryBody = `{{ . | pipelineDot }} {{ .PipelineName }} {{ if hasPipelineRun . }}({{ . | pipelineLastExecution}}){{ end }}`

const reset = "\033[0m"
const black = "235"
const white = "231"
const green = "34"
const red = "9"
const blue = "4"
const yellow = "172"
const grey = "247"

func bg(color string) string { return fmt.Sprintf("\033[48;5;%sm", color) }
func fg(color string) string { return fmt.Sprintf("\033[38;5;%sm", color) }

type templates struct {
	Pipeline        *template.Template
	PipelineSummary *template.Template
}

var defaultTemplates templates

func init() {
	summaryTemplate, err := template.New("CodePipelineSummary").Funcs(FuncMap).Parse(fmt.Sprintf("%s\n", pipelineSummaryBody))
	if err != nil {
		panic(err)
	}
	pipelineTemplate, err := template.New("CodePipelineSummary").Funcs(FuncMap).Parse(fmt.Sprintf("%s\n", pipelineBody))
	if err != nil {
		panic(err)
	}
	defaultTemplates = templates{
		Pipeline:        pipelineTemplate,
		PipelineSummary: summaryTemplate,
	}
}

func Bold(data string) string {
	return fmt.Sprintf("\033[1m%s\033[0m", data)
}
func Underline(data string) string {
	return fmt.Sprintf("\033[4m%s\033[0m", data)
}
func Grey(data string) string {
	return fmt.Sprintf("%s%s%s", fg(grey), data, reset)
}
func Blue(data string) string {
	return fmt.Sprintf("%s%s%s", fg(blue), data, reset)
}
func Red(data string) string {
	return fmt.Sprintf("%s%s%s", fg(red), data, reset)
}
func Green(data string) string {
	return fmt.Sprintf("%s%s%s", fg(green), data, reset)
}

var FuncMap = template.FuncMap{
	"bold": func(data string) string {
		return Bold(data)
	},
	"underline": func(data string) string {
		return Underline(data)
	},
	"isNotLastAction": func(current int, actions []types.ActionState) bool {
		return current < len(actions)-1
	},
	"isActionDetailRequired": func(action *types.StageState) bool {
		return action.LatestExecution == nil || action.LatestExecution.Status != types.StageExecutionStatusSucceeded
	},
	"stageDot": func(action *types.StageState) string {
		if action.LatestExecution != nil {
			switch action.LatestExecution.Status {
			case types.StageExecutionStatusInprogress:
				return Blue("•")
			case types.StageExecutionStatusSucceeded:
				return Green("✓")
			case types.StageExecutionStatusFailed:
				return Red("✗")
			case types.StageExecutionStatusStopped:
				return Red("✗")
			case types.StageExecutionStatusStopping:
				return Red("✗")
			}
		}
		return "•"
	},
	"pipelineDot": func(pipeline *codepipeline.GetPipelineStateOutput) string {
		idx := len(pipeline.StageStates)
		for idx > 0 {
			idx--
			stage := pipeline.StageStates[idx]
			if stage.LatestExecution != nil {
				switch stage.LatestExecution.Status {
				case types.StageExecutionStatusInprogress:
					return Blue("•")
				case types.StageExecutionStatusSucceeded:
					return Green("✓")
				case types.StageExecutionStatusFailed:
					return Red("✗")
				case types.StageExecutionStatusStopped:
					return Red("✗")
				case types.StageExecutionStatusStopping:
					return Red("✗")
				}
			}
		}
		return "•"
	},
	"actionDot": func(action *types.ActionState) string {
		if action.LatestExecution != nil {
			switch action.LatestExecution.Status {
			case types.ActionExecutionStatusInprogress:
				return Blue("→")
			case types.ActionExecutionStatusSucceeded:
				return Green("✓")
			case types.ActionExecutionStatusFailed:
				return Red("✗")
			case types.ActionExecutionStatusAbandoned:
				return Red("✗")
			}
		}
		return "•"
	},
	"stageLastExecution": func(stage *types.StageState) string {
		lastAction := stage.ActionStates[len(stage.ActionStates)-1]
		return Grey(humanize.Time(*lastAction.LatestExecution.LastStatusChange))
	},
	"pipelineLastExecution": func(pipeline *codepipeline.GetPipelineStateOutput) string {
		idx := len(pipeline.StageStates)
		for idx > 0 {
			idx--
			stage := pipeline.StageStates[idx]
			if stage.LatestExecution != nil {
				actionsIdx := len(stage.ActionStates)
				for actionsIdx > 0 {
					actionsIdx--
					lastAction := stage.ActionStates[actionsIdx]
					if lastAction.LatestExecution != nil && lastAction.LatestExecution.LastStatusChange != nil {
						return Grey(humanize.Time(*lastAction.LatestExecution.LastStatusChange))
					}
				}
			}
		}
		return ""
	},
	"actionLastExecution": func(action *types.ActionState) string {
		if action.LatestExecution == nil {
			return ""
		}
		return Grey(humanize.Time(*action.LatestExecution.LastStatusChange))
	},
	"hasActionRun": func(action *types.ActionState) bool {
		return action.LatestExecution != nil && action.LatestExecution.LastStatusChange != nil
	},
	"hasPipelineRun": func(pipeline *codepipeline.GetPipelineStateOutput) bool {
		idx := len(pipeline.StageStates)
		for idx > 0 {
			idx--
			stage := pipeline.StageStates[idx]
			if stage.LatestExecution != nil {
				return true
			}
		}
		return false
	},
}

func WritePipelineState(input *codepipeline.GetPipelineStateOutput, w io.Writer) error {
	return defaultTemplates.Pipeline.Execute(w, input)
}
func WritePipelineSummary(input *codepipeline.GetPipelineStateOutput, w io.Writer) error {
	return defaultTemplates.PipelineSummary.Execute(w, input)
}

func main() {
	// SSO not supported for now
	//https://github.com/aws/aws-sdk-go-v2/issues/705

	cfg, err := config.LoadDefaultConfig()
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}
	codepipelineClient := codepipeline.NewFromConfig(cfg)
	rootCmd := &cobra.Command{
		Use: "pipeline-cli",
	}
	rootCmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Args:    cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			var nextToken *string
			for {
				out, err := codepipelineClient.ListPipelines(cmd.Context(), &codepipeline.ListPipelinesInput{
					NextToken: nextToken,
				})
				if err != nil {
					log.Fatalf("failed to list pipeline, %v", err)
				}
				for _, pipelineSummary := range out.Pipelines {
					out, err := codepipelineClient.GetPipelineState(cmd.Context(), &codepipeline.GetPipelineStateInput{
						Name: pipelineSummary.Name,
					})
					if err != nil {
						log.Fatalf("failed to load pipeline state: %v", err)
						continue
					}
					err = WritePipelineSummary(out, cmd.OutOrStdout())
					if err != nil {
						log.Fatalf("failed to render pipeline summary, %v", err)
					}
				}
				if out.NextToken == nil {
					return
				}
				nextToken = out.NextToken
			}
		},
	})
	rootCmd.AddCommand(&cobra.Command{
		Use:  "get",
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			out, err := codepipelineClient.GetPipelineState(cmd.Context(), &codepipeline.GetPipelineStateInput{Name: aws.String(args[0])})
			if err != nil {
				log.Fatalf("failed to load pipeline state, %v", err)
			}
			err = WritePipelineState(out, cmd.OutOrStdout())
			if err != nil {
				log.Fatalf("failed to render pipeline state, %v", err)
			}
		},
	})
	rootCmd.AddCommand(&cobra.Command{
		Use:  "watch",
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			writer := uilive.New()
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for {
				out, err := codepipelineClient.GetPipelineState(cmd.Context(), &codepipeline.GetPipelineStateInput{Name: aws.String(args[0])})
				if err != nil {
					log.Fatalf("failed to load pipeline state, %v", err)
				}
				err = WritePipelineState(out, writer)
				if err != nil {
					log.Fatalf("failed to render pipeline state, %v", err)
				}
				writer.Flush()
				<-ticker.C
			}
		},
	})
	rootCmd.AddCommand(&cobra.Command{
		Use:  "trigger",
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			_, err := codepipelineClient.StartPipelineExecution(cmd.Context(), &codepipeline.StartPipelineExecutionInput{Name: aws.String(args[0])})
			if err != nil {
				log.Fatalf("failed to trigger pipeline: %v", err)
			}
			fmt.Printf("%s pipeline started\n", Bold(args[0]))
		},
	})
	rootCmd.Execute()
}
