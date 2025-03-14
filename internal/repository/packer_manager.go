package repository

import (
	"context"
	"hash"
	"io"
	"os"
	"runtime"
	"sync"

	"github.com/restic/restic/internal/errors"
	"github.com/restic/restic/internal/hashing"
	"github.com/restic/restic/internal/restic"

	"github.com/restic/restic/internal/crypto"
	"github.com/restic/restic/internal/debug"
	"github.com/restic/restic/internal/fs"
	"github.com/restic/restic/internal/pack"

	"github.com/minio/sha256-simd"
)

// Saver implements saving data in a backend.
type Saver interface {
	Save(context.Context, restic.Handle, restic.RewindReader) error
	Hasher() hash.Hash
}

// Packer holds a pack.Packer together with a hash writer.
type Packer struct {
	*pack.Packer
	hw      *hashing.Writer
	beHw    *hashing.Writer
	tmpfile *os.File
}

// packerManager keeps a list of open packs and creates new on demand.
type packerManager struct {
	be      Saver
	key     *crypto.Key
	pm      sync.Mutex
	packers []*Packer
}

const minPackSize = 4 * 1024 * 1024

// newPackerManager returns an new packer manager which writes temporary files
// to a temporary directory
func newPackerManager(be Saver, key *crypto.Key) *packerManager {
	return &packerManager{
		be:  be,
		key: key,
	}
}

// findPacker returns a packer for a new blob of size bytes. Either a new one is
// created or one is returned that already has some blobs.
func (r *packerManager) findPacker() (packer *Packer, err error) {
	r.pm.Lock()
	defer r.pm.Unlock()

	// search for a suitable packer
	if len(r.packers) > 0 {
		p := r.packers[0]
		last := len(r.packers) - 1
		r.packers[0] = r.packers[last]
		r.packers[last] = nil // Allow GC of stale reference.
		r.packers = r.packers[:last]
		return p, nil
	}

	// no suitable packer found, return new
	debug.Log("create new pack")
	tmpfile, err := fs.TempFile("", "restic-temp-pack-")
	if err != nil {
		return nil, errors.Wrap(err, "fs.TempFile")
	}

	w := io.Writer(tmpfile)
	beHasher := r.be.Hasher()
	var beHw *hashing.Writer
	if beHasher != nil {
		beHw = hashing.NewWriter(w, beHasher)
		w = beHw
	}

	hw := hashing.NewWriter(w, sha256.New())
	p := pack.NewPacker(r.key, hw)
	packer = &Packer{
		Packer:  p,
		beHw:    beHw,
		hw:      hw,
		tmpfile: tmpfile,
	}

	return packer, nil
}

// insertPacker appends p to s.packs.
func (r *packerManager) insertPacker(p *Packer) {
	r.pm.Lock()
	defer r.pm.Unlock()

	r.packers = append(r.packers, p)
	debug.Log("%d packers\n", len(r.packers))
}

// savePacker stores p in the backend.
func (r *Repository) savePacker(ctx context.Context, t restic.BlobType, p *Packer) error {
	debug.Log("save packer for %v with %d blobs (%d bytes)\n", t, p.Packer.Count(), p.Packer.Size())
	_, err := p.Packer.Finalize()
	if err != nil {
		return err
	}

	id := restic.IDFromHash(p.hw.Sum(nil))
	h := restic.Handle{Type: restic.PackFile, Name: id.String(),
		ContainedBlobType: t}
	var beHash []byte
	if p.beHw != nil {
		beHash = p.beHw.Sum(nil)
	}
	rd, err := restic.NewFileReader(p.tmpfile, beHash)
	if err != nil {
		return err
	}

	err = r.be.Save(ctx, h, rd)
	if err != nil {
		debug.Log("Save(%v) error: %v", h, err)
		return err
	}

	debug.Log("saved as %v", h)

	err = p.tmpfile.Close()
	if err != nil {
		return errors.Wrap(err, "close tempfile")
	}

	// on windows the tempfile is automatically deleted on close
	if runtime.GOOS != "windows" {
		err = fs.RemoveIfExists(p.tmpfile.Name())
		if err != nil {
			return errors.Wrap(err, "Remove")
		}
	}

	// update blobs in the index
	debug.Log("  updating blobs %v to pack %v", p.Packer.Blobs(), id)
	r.idx.StorePack(id, p.Packer.Blobs())

	// Save index if full
	if r.noAutoIndexUpdate {
		return nil
	}
	return r.SaveFullIndex(ctx)
}

// countPacker returns the number of open (unfinished) packers.
func (r *packerManager) countPacker() int {
	r.pm.Lock()
	defer r.pm.Unlock()

	return len(r.packers)
}
