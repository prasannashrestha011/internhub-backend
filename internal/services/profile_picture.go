package services

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
)

const maxProfilePictureSize = 5 << 20

var ErrInvalidProfilePicture = errors.New("upload a JPEG, PNG or WebP image of at most 5 MiB")

func readProfilePicture(file *multipart.FileHeader) ([]byte, string, string, error) {
	if file == nil || file.Size <= 0 || file.Size > maxProfilePictureSize {
		return nil, "", "", ErrInvalidProfilePicture
	}
	reader, err := file.Open()
	if err != nil {
		return nil, "", "", err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxProfilePictureSize+1))
	if err != nil {
		return nil, "", "", err
	}
	if len(data) == 0 || len(data) > maxProfilePictureSize {
		return nil, "", "", ErrInvalidProfilePicture
	}
	contentType := http.DetectContentType(data)
	var extension string
	switch contentType {
	case "image/jpeg":
		extension = ".jpg"
	case "image/png":
		extension = ".png"
	case "image/webp":
		extension = ".webp"
	default:
		return nil, "", "", ErrInvalidProfilePicture
	}
	return data, contentType, extension, nil
}
