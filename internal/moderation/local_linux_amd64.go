//go:build linux && amd64

package moderation

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"image"
	"sync"
	"time"

	"github.com/mackross/gonnx"
	_ "github.com/mackross/gonnx/runtimes/linuxamd64"
	ort "github.com/shota3506/onnxruntime-purego/onnxruntime"
)

//go:embed assets/image-safety-classifier-xs.onnx
var modelONNX []byte

var (
	localOnce    sync.Once
	localChecker *localClassifier
	localErr     error
)

func New() (Checker, error) {
	localOnce.Do(func() {
		sess, err := gonnx.OpenReader(bytes.NewReader(modelONNX),
			gonnx.WithLogID("townsquare-moderation"),
			gonnx.WithThreads(2))
		if err != nil {
			localErr = fmt.Errorf("load image safety model: %w", err)
			return
		}
		inputs := sess.InputNames()
		outputs := sess.OutputNames()
		if len(inputs) != 1 || len(outputs) != 1 {
			sess.Close()
			localErr = fmt.Errorf("unexpected image safety model signature: inputs=%v outputs=%v", inputs, outputs)
			return
		}
		localChecker = &localClassifier{sess: sess, input: inputs[0], output: outputs[0]}
	})
	return localChecker, localErr
}

type localClassifier struct {
	mu     sync.Mutex
	sess   *gonnx.Session
	input  string
	output string
}

func (c *localClassifier) Check(img image.Image) (Verdict, error) {
	data := pixels(img)
	c.mu.Lock()
	defer c.mu.Unlock()

	input, err := gonnx.Tensor(c.sess.Runtime, data, 1, 3, InputSize, InputSize)
	if err != nil {
		return Verdict{}, fmt.Errorf("create input tensor: %w", err)
	}
	defer input.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outputs, err := c.sess.Run(ctx, map[string]*ort.Value{c.input: input})
	if err != nil {
		return Verdict{}, fmt.Errorf("run image safety model: %w", err)
	}
	for name, value := range outputs {
		if name != c.output {
			value.Close()
		}
	}
	out, ok := outputs[c.output]
	if !ok {
		return Verdict{}, fmt.Errorf("image safety model did not return %q", c.output)
	}
	defer out.Close()
	probs, shape, err := gonnx.TensorData[float32](out)
	if err != nil {
		return Verdict{}, fmt.Errorf("read image safety scores: %w", err)
	}
	if len(probs) != 3 || len(shape) != 2 || shape[1] != 3 {
		return Verdict{}, fmt.Errorf("unexpected image safety output shape: %v", shape)
	}
	return Verdict{NSFL: float64(probs[0]), NSFW: float64(probs[1]), SFW: float64(probs[2])}, nil
}
