#!/usr/bin/env bash

set -euo pipefail

usage() {
    cat <<'EOF'
Usage: AFX_SDK_ROOT=/path/to/Audio_Effects_SDK scripts/afx-smoke-test.sh INPUT.wav [OUTPUT.wav]

Runs NVIDIA's AFX 2.1.0 sample application with the 48 kHz denoiser and the
L40 model selector. L40 and the GeForce RTX 4080 SUPER both use SM 8.9.

The input must be a 48 kHz WAV file. The default output is written beside the
input as INPUT.afx-denoised.wav.

Set AFX_INTENSITY to choose the suppression strength from 0.0 to 1.0.
The default is 0.90.
EOF
}

if [[ ${1:-} == "-h" || ${1:-} == "--help" ]]; then
    usage
    exit 0
fi

if [[ $# -lt 1 || $# -gt 2 ]]; then
    usage >&2
    exit 2
fi

if [[ -z ${AFX_SDK_ROOT:-} ]]; then
    echo "error: AFX_SDK_ROOT must point to the extracted Audio_Effects_SDK directory" >&2
    exit 1
fi

intensity=${AFX_INTENSITY:-0.90}
if ! [[ $intensity =~ ^(0(\.[0-9]+)?|1(\.0+)?|\.[0-9]+)$ ]]; then
    echo "error: AFX_INTENSITY must be a number from 0.0 to 1.0: $intensity" >&2
    exit 1
fi

input=$1
if [[ ! -f $input ]]; then
    echo "error: input WAV does not exist: $input" >&2
    exit 1
fi

input=$(realpath "$input")
input_name=$(basename "$input")
input_stem=${input_name%.*}
output=${2:-"$(dirname "$input")/${input_stem}.afx-denoised.wav"}
output=$(realpath -m "$output")

if [[ $input == "$output" ]]; then
    echo "error: output must not overwrite the input WAV" >&2
    exit 1
fi
if [[ -e $output ]]; then
    echo "error: output already exists: $output" >&2
    exit 1
fi

sample_dir="$AFX_SDK_ROOT/samples/effects_demo"
builder="$sample_dir/build.sh"
sample="$sample_dir/build/effects_demo"
model="$AFX_SDK_ROOT/features/denoiser/models/sm_89/denoiser_48k.trtpkg"

if [[ ! -x $builder ]]; then
    echo "error: AFX sample builder is missing: $builder" >&2
    echo "download the AFX 2.1.0 samples with: $AFX_SDK_ROOT/samples/download_samples.sh" >&2
    exit 1
fi
if [[ ! -e $model ]]; then
    echo "error: SM 8.9 denoiser model is missing: $model" >&2
    echo "download it from the SDK features directory with:" >&2
    echo "  ./download_features.sh --gpu l40 --effects denoiser-48k" >&2
    exit 1
fi

if [[ ! -x $sample ]]; then
    (
        cd "$sample_dir"
        "$builder"
    )
fi

mkdir -p "$(dirname "$output")"
config_file=$(mktemp /tmp/nv-x-afx-denoiser.XXXXXX.cfg)
trap 'rm -f "$config_file"' EXIT

cat >"$config_file" <<EOF
effect denoiser
sample_rate 48000
model $model
real_time 0
intensity_ratio $intensity
effect_version 1
input_wav_list $input
output_wav_list $output
frame_size 10
EOF

echo "Running AFX 48 kHz denoiser at intensity $intensity on: $input"
"$sample" -c "$config_file"
echo "Wrote denoised WAV: $output"
