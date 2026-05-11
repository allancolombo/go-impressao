package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/goopedir/go-impressao/internal/config"
	"github.com/goopedir/go-impressao/internal/models"
	"github.com/goopedir/go-impressao/internal/services/printer"
)

type PrintService struct {
	logger    *log.Logger
	jobStore  *JobStore
	formatter *Formatter
	printer   printer.Printer
	history   *HistoryStore
	cfg       *config.Manager
}

func NewPrintService(logger *log.Logger, jobStore *JobStore, formatter *Formatter, printer printer.Printer, history *HistoryStore, cfg *config.Manager) *PrintService {
	return &PrintService{
		logger:    logger,
		jobStore: jobStore,
		formatter: formatter,
		printer:   printer,
		history:   history,
		cfg:       cfg,
	}
}

// StartPrint dispara a impressão de forma assíncrona (goroutine) e atualiza o status do job.
func (s *PrintService) StartPrint(jobID string) error {
	job, ok := s.jobStore.Get(jobID)
	if !ok {
		return errors.New("comanda não encontrada")
	}
	payload := job.Payload

	if job.Status == StatusImprimindo {
		return errors.New("esta comanda já está em impressão")
	}
	if job.Status == StatusImpresso {
		return errors.New("esta comanda já foi impressa")
	}

	_ = s.jobStore.Update(jobID, func(j *Job) {
		j.Status = StatusImprimindo
		j.ErrorPublic = ""
	})

	s.logger.Printf("job=%s impressão iniciada impressora_erp=%q impressora_windows=%q", jobID, job.Payload.Impressora, job.Payload.Driver)

	go func() {
		printedAt := time.Now()
		cols := 48
		if s.cfg != nil {
			cols = s.cfg.EffectiveCols(payload.Driver)
		}
		text := s.formatter.FormatComandaCozinhaWithCols(payload, printedAt, cols)

		ctx, cancel := printer.DefaultPrintContext()
		defer cancel()

		err := s.printer.PrintText(ctx, payload.Driver, text)
		if err != nil {
			s.logger.Printf("job=%s erro ao imprimir: %v", jobID, err)
			_ = s.jobStore.Update(jobID, func(j *Job) {
				j.Status = StatusErro
				j.ErrorPublic = fmt.Sprintf("falha ao imprimir: %v", err)
			})
			if s.history != nil {
				_ = s.history.Add(HistoryRecord{
					ID:                "job-" + jobID + "-" + strconv.FormatInt(printedAt.UnixNano(), 10),
					OccurredAt:        printedAt,
					Status:            StatusErro,
					Erro:              err.Error(),
					Canal:             "cozinha",
					Tipo:              string(payload.Tipo),
					Numero:            payload.Numero,
					Usuario:           payload.Usuario,
					ImpressoraERP:     payload.Impressora,
					ImpressoraWindows: payload.Driver,
					Resumo:            BuildResumo(payload),
					TextoImpressao:    text,
					Payload:           payload,
				})
			}
			return
		}

		s.logger.Printf("job=%s impressão concluída", jobID)
		_ = s.jobStore.Update(jobID, func(j *Job) {
			j.Status = StatusImpresso
			j.PrintedAt = &printedAt
		})
		if s.history != nil {
			_ = s.history.Add(HistoryRecord{
				ID:                "job-" + jobID + "-" + strconv.FormatInt(printedAt.UnixNano(), 10),
				OccurredAt:        printedAt,
				Status:            StatusImpresso,
				Canal:             "cozinha",
				Tipo:              string(payload.Tipo),
				Numero:            payload.Numero,
				Usuario:           payload.Usuario,
				ImpressoraERP:     payload.Impressora,
				ImpressoraWindows: payload.Driver,
				Resumo:            BuildResumo(payload),
				TextoImpressao:    text,
				Payload:           payload,
			})
		}
	}()

	return nil
}

// PrintNow imprime de forma síncrona (útil para integrações futuras), respeitando o contexto recebido.
func (s *PrintService) PrintNow(ctx context.Context, jobID string) error {
	job, ok := s.jobStore.Get(jobID)
	if !ok {
		return errors.New("comanda não encontrada")
	}
	payload := job.Payload

	printedAt := time.Now()
	cols := 48
	if s.cfg != nil {
		cols = s.cfg.EffectiveCols(payload.Driver)
	}
	text := s.formatter.FormatComandaCozinhaWithCols(payload, printedAt, cols)

	s.logger.Printf("job=%s impressão síncrona impressora_erp=%q impressora_windows=%q", jobID, job.Payload.Impressora, job.Payload.Driver)
	if err := s.printer.PrintText(ctx, payload.Driver, text); err != nil {
		if s.history != nil {
			_ = s.history.Add(HistoryRecord{
				ID:                "job-" + jobID + "-" + strconv.FormatInt(printedAt.UnixNano(), 10),
				OccurredAt:        printedAt,
				Status:            StatusErro,
				Erro:              err.Error(),
				Canal:             "cozinha",
				Tipo:              string(payload.Tipo),
				Numero:            payload.Numero,
				Usuario:           payload.Usuario,
				ImpressoraERP:     payload.Impressora,
				ImpressoraWindows: payload.Driver,
				Resumo:            BuildResumo(payload),
				TextoImpressao:    text,
				Payload:           payload,
			})
		}
		return err
	}

	_ = s.jobStore.Update(jobID, func(j *Job) {
		j.Status = StatusImpresso
		j.PrintedAt = &printedAt
	})
	if s.history != nil {
		_ = s.history.Add(HistoryRecord{
			ID:                "job-" + jobID + "-" + strconv.FormatInt(printedAt.UnixNano(), 10),
			OccurredAt:        printedAt,
			Status:            StatusImpresso,
			Canal:             "cozinha",
			Tipo:              string(payload.Tipo),
			Numero:            payload.Numero,
			Usuario:           payload.Usuario,
			ImpressoraERP:     payload.Impressora,
			ImpressoraWindows: payload.Driver,
			Resumo:            BuildResumo(payload),
			TextoImpressao:    text,
			Payload:           payload,
		})
	}
	return nil
}

func (s *PrintService) PrintTest(ctx context.Context, printerName string) error {
	printerName = strings.TrimSpace(printerName)
	if printerName == "" {
		return errors.New("driver é obrigatório")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	tester, ok := s.printer.(interface {
		PrintTest(ctx context.Context, printerName string) error
	})
	if !ok {
		return errors.New("impressão de teste não suportada")
	}
	return tester.PrintTest(ctx, printerName)
}

func (s *PrintService) PrintConferencia(ctx context.Context, req models.ConferenciaRequest) error {
	tester, ok := s.printer.(interface {
		PrintConferencia(ctx context.Context, req models.ConferenciaRequest) error
	})
	if !ok {
		return errors.New("impressão de conferência não suportada")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return tester.PrintConferencia(ctx, req)
}

func (s *PrintService) PrintSangria(ctx context.Context, req models.SangriaRequest) error {
	tester, ok := s.printer.(interface {
		PrintSangria(ctx context.Context, req models.SangriaRequest) error
	})
	if !ok {
		return errors.New("impressão de sangria não suportada")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return tester.PrintSangria(ctx, req)
}

func (s *PrintService) PrintCaixaFechamento(ctx context.Context, req models.CaixaFechamentoRequest) error {
	tester, ok := s.printer.(interface {
		PrintCaixaFechamento(ctx context.Context, req models.CaixaFechamentoRequest) error
	})
	if !ok {
		return errors.New("impressão de fechamento de caixa não suportada")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return tester.PrintCaixaFechamento(ctx, req)
}
