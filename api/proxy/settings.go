package proxy

import (
	"context"
	"time"

	proxyService "proxy-hub/service/proxy"
)

type settingsExportOutput struct {
	Body proxyService.SettingsBackupDTO `json:"body"`
}

func settingsExportHandler(ctx context.Context, _ *struct{}) (*settingsExportOutput, error) {
	backup, err := proxyService.SettingsExport(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	return &settingsExportOutput{Body: *backup}, nil
}

type settingsExportZipOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte `json:"body"`
}

func settingsExportZipHandler(ctx context.Context, _ *struct{}) (*settingsExportZipOutput, error) {
	backup, err := proxyService.SettingsExport(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	body, err := proxyService.SettingsBackupToZip(backup)
	if err != nil {
		return nil, mapError(err)
	}
	return &settingsExportZipOutput{
		ContentType:        "application/zip",
		ContentDisposition: "attachment; filename=" + settingsBackupZipFileName(backup.ExportedAt),
		Body:               body,
	}, nil
}

type settingsImportInput struct {
	Body proxyService.SettingsBackupDTO
}

type settingsImportOutput struct {
	Body proxyService.SettingsImportResultDTO `json:"body"`
}

func settingsImportHandler(ctx context.Context, input *settingsImportInput) (*settingsImportOutput, error) {
	result, err := proxyService.SettingsImport(ctx, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	return &settingsImportOutput{Body: *result}, nil
}

type settingsImportZipInput struct {
	RawBody []byte `contentType:"application/zip"`
}

func settingsImportZipHandler(ctx context.Context, input *settingsImportZipInput) (*settingsImportOutput, error) {
	result, err := proxyService.SettingsImportZip(ctx, input.RawBody)
	if err != nil {
		return nil, mapError(err)
	}
	return &settingsImportOutput{Body: *result}, nil
}

func settingsBackupZipFileName(exportedAt time.Time) string {
	if exportedAt.IsZero() {
		exportedAt = time.Now().UTC()
	}
	return "proxyhub-settings-" + exportedAt.UTC().Format("20060102-150405") + ".zip"
}
