package gocv

import (
	"io"
	"time"
)

type GifOpsSizeMethod int

const (
	GifOpsNoResize GifOpsSizeMethod = iota
	GifOpsFit
	GifOpsResize
)

// GifOptions controls how GifOps resizes and encodes the
// pixel data decoded from a GifDecoder
type GifOptions struct {
	// FileType should be a string starting with '.', e.g.
	// ".jpeg"
	FileType string

	// Width controls the width of the output image
	Width int

	// Height controls the height of the output image
	Height int

	// ResizeMethod controls how the image will be transformed to
	// its output size. Notably, GifOpsFit will do a cropping
	// resize, while GifOpsResize will stretch the image.
	ResizeMethod GifOpsSizeMethod

	// NormalizeOrientation will flip and rotate the image as necessary
	// in order to undo EXIF-based orientation
	// NormalizeOrientation bool

	// EncodeOptions controls the encode quality options
	EncodeOptions map[int]int

	// MaxEncodeFrames controls the maximum number of frames that will be resized
	MaxEncodeFrames int

	// MaxEncodeDuration controls the maximum duration of animated image that will be resized
	MaxEncodeDuration time.Duration
}

// GifOps is a reusable object that can resize and encode images.
type GifOps struct {
	frames     []*Framebuffer
	frameIndex int
}

// NewGifOps creates a new GifOps object that will operate
// on images up to maxSize on each axis.
func NewGifOps(maxSize int) *GifOps {
	frames := make([]*Framebuffer, 2)
	frames[0] = NewFramebuffer(maxSize, maxSize)
	frames[1] = NewFramebuffer(maxSize, maxSize)
	return &GifOps{
		frames:     frames,
		frameIndex: 0,
	}
}

func (o *GifOps) active() *Framebuffer {
	return o.frames[o.frameIndex]
}

func (o *GifOps) secondary() *Framebuffer {
	return o.frames[1-o.frameIndex]
}

func (o *GifOps) swap() {
	o.frameIndex = 1 - o.frameIndex
}

// Clear resets all pixel data in GifOps. This need not be called
// between calls to Transform. You may choose to call this to remove
// image data from memory.
func (o *GifOps) Clear() {
	o.frames[0].Clear()
	o.frames[1].Clear()
}

// Close releases resources associated with GifOps
func (o *GifOps) Close() {
	o.frames[0].Close()
	o.frames[1].Close()
}

func (o *GifOps) decode(d GifDecoder) error {
	active := o.active()
	return d.DecodeTo(active)
}

func (o *GifOps) fit(d GifDecoder, width, height int) (bool, error) {
	active := o.active()
	secondary := o.secondary()
	err := active.Fit(width, height, secondary)
	if err != nil {
		return false, err
	}
	o.swap()
	return true, nil
}

func (o *GifOps) resize(d GifDecoder, width, height int) (bool, error) {
	active := o.active()
	secondary := o.secondary()
	err := active.ResizeTo(width, height, secondary)
	if err != nil {
		return false, err
	}
	o.swap()
	return true, nil
}

// func (o *GifOps) normalizeOrientation(orientation ImageOrientation) {
// 	active := o.active()
// 	active.OrientationTransform(orientation)
// }

func (o *GifOps) encode(e GifEncoder, opt map[int]int) ([]byte, error) {
	active := o.active()
	return e.Encode(active, opt)
}

func (o *GifOps) encodeEmpty(e GifEncoder, opt map[int]int) ([]byte, error) {
	return e.Encode(nil, opt)
}

func (o *GifOps) skipToEnd(d GifDecoder) error {
	var err error
	for {
		err = d.SkipFrame()
		if err != nil {
			return err
		}
	}
}

// Transform performs the requested transform operations on the GifDecoder specified by d.
// The result is written into the output buffer dst. A new slice pointing to dst is returned
// with its length set to the length of the resulting image. Errors may occur if the decoded
// image is too large for GifOps or if Encoding fails.
//
// It is important that .Decode() not have been called already on d.
func (o *GifOps) Transform(d GifDecoder, opt *GifOptions, dst []byte) ([]byte, error) {
	// h, err := d.Header()
	// if err != nil {
	// 	return nil, err
	// }

	enc, err := NewGifEncoder(opt.FileType, d, dst)
	if err != nil {
		return nil, err
	}
	defer enc.Close()

	frameCount := 0
	duration := time.Duration(0)

	for {
		err = o.decode(d)
		emptyFrame := false
		if err != nil {
			if err != io.EOF {
				return nil, err
			}
			// io.EOF means we are out of frames, so we should signal to Gifencoder to wrap up
			emptyFrame = true
		}

		duration += o.active().Duration()

		if opt.MaxEncodeDuration != 0 && duration > opt.MaxEncodeDuration {
			err = o.skipToEnd(d)
			if err != io.EOF {
				return nil, err
			}
			return o.encodeEmpty(enc, opt.EncodeOptions)
		}

		// o.normalizeOrientation(h.Orientation())

		var swapped bool
		if opt.ResizeMethod == GifOpsFit {
			swapped, err = o.fit(d, opt.Width, opt.Height)
		} else if opt.ResizeMethod == GifOpsResize {
			swapped, err = o.resize(d, opt.Width, opt.Height)
		} else {
			swapped, err = false, nil
		}

		if err != nil {
			return nil, err
		}

		var content []byte
		if emptyFrame {
			content, err = o.encodeEmpty(enc, opt.EncodeOptions)
		} else {
			content, err = o.encode(enc, opt.EncodeOptions)
		}

		if err != nil {
			return nil, err
		}

		if content != nil {
			return content, nil
		}

		frameCount++

		if opt.MaxEncodeFrames != 0 && frameCount == opt.MaxEncodeFrames {
			err = o.skipToEnd(d)
			if err != io.EOF {
				return nil, err
			}
			return o.encodeEmpty(enc, opt.EncodeOptions)
		}

		// content == nil and err == nil -- this is Gifencoder telling us to do another frame

		// for mulitple frames/gifs we need the decoded frame to be active again
		if swapped {
			o.swap()
		}
	}
}
