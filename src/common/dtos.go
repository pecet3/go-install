package common

type GoRelease struct {
	Version string `json:"version"`
	Files   []struct {
		Filename string `json:"filename"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Kind     string `json:"kind"`
		Sha256   string `json:"sha256"`
	} `json:"files"`
}
