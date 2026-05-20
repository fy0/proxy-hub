package proxy

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path"
	"strings"
	"time"

	"proxy-hub/model"
)

const (
	settingsBackupZipEntryName       = "proxyhub-settings.json"
	settingsBackupMaxJSONBytes int64 = 64 << 20
)

func SettingsExportZip(ctx context.Context, tx model.DBTx) ([]byte, error) {
	backup, err := SettingsExport(ctx, tx)
	if err != nil {
		return nil, err
	}
	return SettingsBackupToZip(backup)
}

func SettingsImportZip(ctx context.Context, rawZip []byte) (*SettingsImportResultDTO, error) {
	backup, err := SettingsBackupFromZip(rawZip)
	if err != nil {
		return nil, err
	}
	return SettingsImport(ctx, *backup)
}

func SettingsBackupToZip(backup *SettingsBackupDTO) ([]byte, error) {
	if backup == nil {
		return nil, invalidSettingsBackup("settings backup is empty")
	}

	payload, err := json.Marshal(backup)
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	header := &zip.FileHeader{
		Name:   settingsBackupZipEntryName,
		Method: zip.Deflate,
	}
	if backup.ExportedAt.IsZero() {
		header.Modified = time.Now().UTC()
	} else {
		header.Modified = backup.ExportedAt
	}
	header.SetMode(0o600)

	entry, err := archive.CreateHeader(header)
	if err != nil {
		_ = archive.Close()
		return nil, err
	}
	if _, err := entry.Write(payload); err != nil {
		_ = archive.Close()
		return nil, err
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func SettingsBackupFromZip(rawZip []byte) (*SettingsBackupDTO, error) {
	reader, err := zip.NewReader(bytes.NewReader(rawZip), int64(len(rawZip)))
	if err != nil {
		return nil, invalidSettingsBackup("zip backup is invalid")
	}

	jsonFile, err := singleSettingsBackupJSONFile(reader.File)
	if err != nil {
		return nil, err
	}
	if jsonFile.UncompressedSize64 > uint64(settingsBackupMaxJSONBytes) {
		return nil, invalidSettingsBackup("settings backup json is too large")
	}

	file, err := jsonFile.Open()
	if err != nil {
		return nil, invalidSettingsBackup("settings backup json cannot be opened")
	}
	defer file.Close()

	payload, err := io.ReadAll(io.LimitReader(file, settingsBackupMaxJSONBytes+1))
	if err != nil {
		return nil, invalidSettingsBackup("settings backup json cannot be read")
	}
	if int64(len(payload)) > settingsBackupMaxJSONBytes {
		return nil, invalidSettingsBackup("settings backup json is too large")
	}

	var backup SettingsBackupDTO
	if err := json.Unmarshal(payload, &backup); err != nil {
		return nil, invalidSettingsBackup("settings backup json is invalid")
	}
	if _, err := validateSettingsBackup(backup); err != nil {
		return nil, err
	}
	return &backup, nil
}

func singleSettingsBackupJSONFile(files []*zip.File) (*zip.File, error) {
	var jsonFile *zip.File
	for _, file := range files {
		if file == nil || file.FileInfo().IsDir() {
			continue
		}
		if !strings.EqualFold(path.Ext(file.Name), ".json") {
			continue
		}
		if jsonFile != nil {
			return nil, invalidSettingsBackup("zip backup contains multiple json files")
		}
		jsonFile = file
	}
	if jsonFile == nil {
		return nil, invalidSettingsBackup("zip backup does not contain a json backup")
	}
	return jsonFile, nil
}
