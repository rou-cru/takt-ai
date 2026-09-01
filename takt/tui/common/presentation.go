package common

// Presentation is the shared result or error state shown by lifecycle flows.
type Presentation struct {
	Message string
	Err     error
}

// Success creates a successful presentation.
func Success(message string) Presentation {
	return Presentation{Message: message}
}

// Failure creates an error presentation.
func Failure(err error) Presentation {
	return Presentation{Err: err}
}
