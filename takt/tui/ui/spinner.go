package ui

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerFrameCount is the number of frames in a full revolution.
var SpinnerFrameCount = len(spinnerFrames)

// Spinner returns the frame at frame, wrapping in both directions.
func Spinner(frame int) string {
	return spinnerFrames[((frame%SpinnerFrameCount)+SpinnerFrameCount)%SpinnerFrameCount]
}
