package utils

import "errors"

var (
	ErrMarkdownDirectoryRequired = errors.New("markdown directory is required")
	ErrMarkdownFilenameRequired  = errors.New("markdown filename is required")
	ErrMarkdownFilenameHasPath   = errors.New("markdown filename must not include path separators")
	ErrMarkdownFilenameExtension = errors.New("markdown filename must end with .md or .markdown")
)
