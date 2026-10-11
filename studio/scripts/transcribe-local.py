"""Short-clip CPU transcription for the opt-in localhost preview only."""
import json
import subprocess
import sys
from pathlib import Path

import numpy as np
import torch
import whisper

runtime, source, destination = map(Path, sys.argv[1:4])
model_path = runtime / "models" / "base.pt"
ffmpeg = runtime / "bin" / "ffmpeg.exe"
if not model_path.is_file() or not ffmpeg.is_file():
    raise RuntimeError("Local model or decoder missing; downloads are disabled")

decoded = subprocess.run(
    [str(ffmpeg), "-nostdin", "-v", "error", "-i", str(source), "-t", "61",
     "-f", "f32le", "-ac", "1", "-ar", "16000", "-"],
    capture_output=True, check=True, timeout=15, creationflags=subprocess.CREATE_NO_WINDOW,
)
audio = np.frombuffer(decoded.stdout, np.float32).copy()
if len(audio) > 60 * 16000:
    raise ValueError("Clip exceeds 60 seconds")
text = ""
if len(audio) and np.max(np.abs(audio)) >= 0.001:
    torch.set_num_threads(2)
    model = whisper.load_model(str(model_path), device="cpu")
    result = model.transcribe(audio, language="zh", fp16=False, verbose=None,
                              condition_on_previous_text=False, temperature=0)
    text = result["text"].strip()
destination.write_text(json.dumps({"text": text}, ensure_ascii=False), encoding="utf-8")
