package thesaurus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// We pin the sources to fixed commits so builds are reproducible. Tests
// point these at a local server.
var (
	thesaurusBaseURL = "https://raw.githubusercontent.com/LibreOffice/dictionaries/c0865a85eb67dc377f2cf8e8fe2c4fa9e53f0dcb/"
	frequencyBaseURL = "https://raw.githubusercontent.com/hermitdave/FrequencyWords/525f9b560de45753a5ea01069454e72e9aa541c6/content/2018/"
	maxDownloadSize  = int64(256 << 20)
	httpClient       = newHTTPClient()
)

func newHTTPClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	t.TLSHandshakeTimeout = 30 * time.Second
	t.ResponseHeaderTimeout = time.Minute
	return &http.Client{Transport: t, Timeout: 10 * time.Minute}
}

// Dir returns the directory of the cached databases: $MYOO_CACHE_DIR if it's
// set, otherwise myoo/thesaurus in the user cache directory.
func Dir() (string, error) {
	if d := os.Getenv("MYOO_CACHE_DIR"); d != "" {
		return d, nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("unable to locate the cache directory, set MYOO_CACHE_DIR: %w", err)
	}
	return filepath.Join(d, "myoo", "thesaurus"), nil
}

// Path returns the database path for a language.
func Path(lang string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, lang+".thesaurus"), nil
}

// Open opens the cached database of lang. If it's missing, damaged or from
// another format version, we download and build it first.
func Open(ctx context.Context, lang string) (*DB, error) {
	path, err := Path(lang)
	if err != nil {
		return nil, err
	}
	db, err := OpenFile(path)
	if err == nil {
		return db, nil
	}
	if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, errFormat) {
		return nil, fmt.Errorf("unable to open the synonym database: %w", err)
	}
	if path, err = Download(ctx, lang); err != nil {
		return nil, err
	}
	return OpenFile(path)
}

// Download fetches the thesaurus and the frequency list of lang, builds the
// database and atomically replaces the cached file. It returns the path.
func Download(ctx context.Context, lang string) (string, error) {
	l, ok := Lookup(lang)
	if !ok {
		return "", fmt.Errorf("unsupported language `%s`", lang)
	}
	path, err := Path(lang)
	if err != nil {
		return "", err
	}
	thes, err := fetch(ctx, thesaurusBaseURL+l.thesaurus)
	if err != nil {
		return "", fmt.Errorf("unable to download thesaurus for `%s`: %w", lang, err)
	}
	freq, err := fetch(ctx, frequencyBaseURL+l.Code+"/"+l.Code+"_50k.txt")
	if err != nil {
		return "", fmt.Errorf("unable to download word frequencies for `%s`: %w", lang, err)
	}
	if err := writeAtomic(path, func(w io.Writer) error {
		return Build(w, lang, bytes.NewReader(thes), bytes.NewReader(freq))
	}); err != nil {
		return "", fmt.Errorf("unable to build the synonym database for `%s`: %w", lang, err)
	}
	return path, nil
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize+1))
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	if int64(len(body)) > maxDownloadSize {
		return nil, fmt.Errorf("GET %s: response larger than %d bytes", url, maxDownloadSize)
	}
	return body, nil
}

// writeAtomic writes to a temporary sibling and only renames it over path
// after fill succeeds, so readers can't see a partial database.
func writeAtomic(path string, fill func(io.Writer) error) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if err := fill(tmp); err != nil {
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
