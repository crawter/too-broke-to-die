// Copyright (C) 2026 crawter
// All rights reserved.

//go:build linux && cgo

package audio

/*
typedef struct stb_vorbis stb_vorbis;
typedef struct stb_vorbis_alloc stb_vorbis_alloc;
extern stb_vorbis *stb_vorbis_open_memory(const unsigned char *, int, int *, const stb_vorbis_alloc *);
extern int stb_vorbis_seek(stb_vorbis *, unsigned int);
extern int stb_vorbis_get_samples_short_interleaved(stb_vorbis *, int, short *, int);
extern int stb_vorbis_get_error(stb_vorbis *);
extern void stb_vorbis_close(stb_vorbis *);
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

// vorbisDecoder uses the stb_vorbis already linked into raylib. Its input is
// pinned and retained until close because the C decoder stores its address.
// PCM transfers use a temporary int16 slice: Linux C short is 16-bit and has
// the same alignment and representation as Go int16. C does not retain it.
type vorbisDecoder struct {
	value *C.stb_vorbis
	pin   runtime.Pinner
	data  []byte
}

func openVorbis(data []byte) (streamDecoder, error) {
	if len(data) == 0 || len(data) > maxAudioBytes {
		return nil, errors.New("invalid compressed audio")
	}
	decoder := &vorbisDecoder{data: data}
	decoder.pin.Pin(&data[0])
	var code C.int
	decoder.value = C.stb_vorbis_open_memory((*C.uchar)(unsafe.Pointer(&data[0])), C.int(len(data)), &code, nil)
	if decoder.value == nil {
		decoder.pin.Unpin()
		return nil, fmt.Errorf("vorbis open: error %d", int(code))
	}
	return decoder, nil
}

func (decoder *vorbisDecoder) read(output []float32) (int, error) {
	if len(output) == 0 || len(output)%2 != 0 || len(output) > decodeChunkFrames*2 {
		return 0, errors.New("invalid decode buffer")
	}
	var samples [decodeChunkFrames * 2]int16
	count := int(C.stb_vorbis_get_samples_short_interleaved(decoder.value, 2, (*C.short)(unsafe.Pointer(&samples[0])), C.int(len(output))))
	code := int(C.stb_vorbis_get_error(decoder.value))
	if code != 0 {
		return 0, fmt.Errorf("vorbis decode: error %d", code)
	}
	for i := 0; i < count*2; i++ {
		output[i] = float32(samples[i]) / 32768
	}
	return count, nil
}

func (decoder *vorbisDecoder) seek(frame int64) error {
	if frame < 0 || frame > 48000*600 {
		return errors.New("invalid vorbis seek")
	}
	if C.stb_vorbis_seek(decoder.value, C.uint(frame)) == 0 {
		return errors.New("vorbis seek failed")
	}
	return nil
}

func (decoder *vorbisDecoder) close() {
	if decoder.value != nil {
		C.stb_vorbis_close(decoder.value)
		decoder.value = nil
		decoder.pin.Unpin()
		decoder.data = nil
	}
}
