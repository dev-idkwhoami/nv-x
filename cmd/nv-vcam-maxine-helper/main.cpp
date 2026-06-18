#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <map>
#include <string>
#include <vector>

#include "nvCVImage.h"
#include "nvCVStatus.h"
#include "nvVFXBackgroundBlur.h"
#include "nvVFXGreenScreen.h"
#include "nvVideoEffects.h"

namespace {

struct ImageBGR {
  int width = 0;
  int height = 0;
  std::vector<unsigned char> pixels;
};

struct Options {
  std::string sdkPath = "/usr/local/VideoFX";
  std::string modelDir = "/usr/local/VideoFX/lib/models";
  std::string input;
  std::string mask;
  std::string blur;
  int width = 0;
  int height = 0;
  int fps = 25;
  float blurStrength = 0.75f;
};

[[noreturn]] void fail(const char *message) {
  std::fprintf(stderr, "error: %s\n", message);
  std::exit(1);
}

[[noreturn]] void failf(const char *prefix, const std::string &value) {
  std::fprintf(stderr, "error: %s%s\n", prefix, value.c_str());
  std::exit(1);
}

void check(NvCV_Status status, const char *step) {
  if (status == NVCV_SUCCESS) {
    return;
  }
  std::fprintf(stderr, "error: %s: %d (%s)\n", step, status,
               NvCV_GetErrorStringFromCode(status));
  std::exit(1);
}

std::map<std::string, std::string> parseFlags(int argc, char **argv, int start) {
  std::map<std::string, std::string> flags;
  for (int i = start; i < argc; ++i) {
    std::string key = argv[i];
    if (key.rfind("--", 0) != 0) {
      failf("unexpected argument: ", key);
    }
    if (i + 1 >= argc) {
      failf("missing value for ", key);
    }
    flags[key.substr(2)] = argv[++i];
  }
  return flags;
}

std::string flag(const std::map<std::string, std::string> &flags,
                 const std::string &name, const std::string &fallback = "") {
  auto it = flags.find(name);
  if (it == flags.end()) {
    return fallback;
  }
  return it->second;
}

void skipWhitespaceAndComments(FILE *f) {
  int c = 0;
  while ((c = std::fgetc(f)) != EOF) {
    if (c == '#') {
      while ((c = std::fgetc(f)) != EOF && c != '\n') {
      }
      continue;
    }
    if (c != ' ' && c != '\n' && c != '\r' && c != '\t') {
      std::ungetc(c, f);
      return;
    }
  }
}

ImageBGR readPPMAsBGR(const std::string &path) {
  FILE *f = std::fopen(path.c_str(), "rb");
  if (!f) {
    failf("open input: ", path);
  }

  char magic[3] = {};
  int width = 0;
  int height = 0;
  int maxValue = 0;
  if (std::fscanf(f, "%2s", magic) != 1 || std::strcmp(magic, "P6") != 0) {
    failf("expected P6 PPM input: ", path);
  }
  skipWhitespaceAndComments(f);
  if (std::fscanf(f, "%d", &width) != 1) {
    fail("read PPM width");
  }
  skipWhitespaceAndComments(f);
  if (std::fscanf(f, "%d", &height) != 1) {
    fail("read PPM height");
  }
  skipWhitespaceAndComments(f);
  if (std::fscanf(f, "%d", &maxValue) != 1) {
    fail("read PPM max value");
  }
  std::fgetc(f);
  if (width < 512 || height < 288 || maxValue != 255) {
    std::fprintf(stderr, "error: expected at least 512x288 PPM max 255, got %dx%d max %d\n",
                 width, height, maxValue);
    std::exit(1);
  }

  std::vector<unsigned char> rgb(static_cast<size_t>(width) * height * 3);
  if (std::fread(rgb.data(), 1, rgb.size(), f) != rgb.size()) {
    fail("read PPM pixels");
  }
  std::fclose(f);

  ImageBGR out;
  out.width = width;
  out.height = height;
  out.pixels.resize(rgb.size());
  for (int i = 0; i < width * height; ++i) {
    out.pixels[i * 3 + 0] = rgb[i * 3 + 2];
    out.pixels[i * 3 + 1] = rgb[i * 3 + 1];
    out.pixels[i * 3 + 2] = rgb[i * 3 + 0];
  }
  return out;
}

void writePGM(const std::string &path, const std::vector<unsigned char> &gray,
              int width, int height) {
  FILE *f = std::fopen(path.c_str(), "wb");
  if (!f) {
    failf("open mask output: ", path);
  }
  std::fprintf(f, "P5\n%d %d\n255\n", width, height);
  if (std::fwrite(gray.data(), 1, gray.size(), f) != gray.size()) {
    failf("write mask output: ", path);
  }
  std::fclose(f);
}

void writePPMFromBGR(const std::string &path, const std::vector<unsigned char> &bgr,
                     int width, int height) {
  FILE *f = std::fopen(path.c_str(), "wb");
  if (!f) {
    failf("open blur output: ", path);
  }
  std::fprintf(f, "P6\n%d %d\n255\n", width, height);
  for (int i = 0; i < width * height; ++i) {
    const unsigned char rgb[3] = {bgr[i * 3 + 2], bgr[i * 3 + 1],
                                  bgr[i * 3 + 0]};
    if (std::fwrite(rgb, 1, sizeof(rgb), f) != sizeof(rgb)) {
      failf("write blur output: ", path);
    }
  }
  std::fclose(f);
}

ImageBGR syntheticBGR() {
  ImageBGR out;
  out.width = 512;
  out.height = 288;
  out.pixels.resize(static_cast<size_t>(out.width) * out.height * 3);
  for (int y = 0; y < out.height; ++y) {
    for (int x = 0; x < out.width; ++x) {
      const int i = (y * out.width + x) * 3;
      out.pixels[i + 0] = static_cast<unsigned char>(32 + (x * 80 / out.width));
      out.pixels[i + 1] = static_cast<unsigned char>(48 + (y * 80 / out.height));
      out.pixels[i + 2] = 160;
    }
  }
  return out;
}

class MaxineProcessor {
public:
  MaxineProcessor(const Options &opts, int width, int height)
      : opts_(opts), width_(width), height_(height),
        input_(static_cast<size_t>(width) * height * 3),
        mask_(static_cast<size_t>(width) * height),
        blurred_(static_cast<size_t>(width) * height * 3) {
    init();
  }

  ~MaxineProcessor() { cleanup(); }

  std::vector<unsigned char> &input() { return input_; }
  std::vector<unsigned char> &mask() { return mask_; }
  std::vector<unsigned char> &blurred() { return blurred_; }

  void run() {
    check(NvCVImage_Transfer(&cpuInput_, &gpuInput_, 1.0f, stream_, &staging_),
          "NvCVImage_Transfer(input CPU->GPU)");
    check(NvVFX_Run(green_, 0), "NvVFX_Run(GreenScreen)");
    check(NvVFX_CudaStreamSynchronize(stream_), "NvVFX_CudaStreamSynchronize(GreenScreen)");
    check(NvVFX_Run(blur_, 0), "NvVFX_Run(BackgroundBlur)");
    check(NvVFX_CudaStreamSynchronize(stream_), "NvVFX_CudaStreamSynchronize(BackgroundBlur)");
    check(NvCVImage_Transfer(&gpuMask_, &cpuMask_, 1.0f, stream_, &staging_),
          "NvCVImage_Transfer(mask GPU->CPU)");
    check(NvCVImage_Transfer(&gpuBlur_, &cpuBlur_, 1.0f, stream_, &staging_),
          "NvCVImage_Transfer(blur GPU->CPU)");
    check(NvVFX_CudaStreamSynchronize(stream_), "NvVFX_CudaStreamSynchronize(output copies)");
  }

private:
  void init() {
    check(NvVFX_CudaStreamCreate(&stream_), "NvVFX_CudaStreamCreate");
    check(NvVFX_CreateEffect(NVVFX_FX_GREEN_SCREEN, &green_), "NvVFX_CreateEffect(GreenScreen)");
    check(NvVFX_SetString(green_, NVVFX_MODEL_DIRECTORY, opts_.modelDir.c_str()),
          "NvVFX_SetString(GreenScreen ModelDir)");
    check(NvVFX_SetCudaStream(green_, NVVFX_CUDA_STREAM, stream_),
          "NvVFX_SetCudaStream(GreenScreen)");
    check(NvVFX_SetU32(green_, NVVFX_MODE, 0), "NvVFX_SetU32(GreenScreen Mode)");
    check(NvVFX_SetU32(green_, NVVFX_MAX_INPUT_WIDTH, width_),
          "NvVFX_SetU32(GreenScreen MaxInputWidth)");
    check(NvVFX_SetU32(green_, NVVFX_MAX_INPUT_HEIGHT, height_),
          "NvVFX_SetU32(GreenScreen MaxInputHeight)");

    check(NvCVImage_Init(&cpuInput_, width_, height_, width_ * 3,
                         input_.data(), NVCV_BGR, NVCV_U8, NVCV_INTERLEAVED,
                         NVCV_CPU),
          "NvCVImage_Init(cpu input)");
    check(NvCVImage_Init(&cpuMask_, width_, height_, width_, mask_.data(),
                         NVCV_A, NVCV_U8, NVCV_INTERLEAVED, NVCV_CPU),
          "NvCVImage_Init(cpu mask)");
    check(NvCVImage_Init(&cpuBlur_, width_, height_, width_ * 3,
                         blurred_.data(), NVCV_BGR, NVCV_U8, NVCV_INTERLEAVED,
                         NVCV_CPU),
          "NvCVImage_Init(cpu blur)");
    check(NvCVImage_Alloc(&gpuInput_, width_, height_, NVCV_BGR, NVCV_U8,
                          NVCV_INTERLEAVED, NVCV_GPU, 0),
          "NvCVImage_Alloc(gpu input)");
    check(NvCVImage_Alloc(&gpuMask_, width_, height_, NVCV_A, NVCV_U8,
                          NVCV_INTERLEAVED, NVCV_GPU, 0),
          "NvCVImage_Alloc(gpu mask)");
    check(NvCVImage_Alloc(&gpuBlur_, width_, height_, NVCV_BGR, NVCV_U8,
                          NVCV_INTERLEAVED, NVCV_GPU, 0),
          "NvCVImage_Alloc(gpu blur)");

    check(NvVFX_SetImage(green_, NVVFX_INPUT_IMAGE, &gpuInput_),
          "NvVFX_SetImage(GreenScreen input)");
    check(NvVFX_SetImage(green_, NVVFX_OUTPUT_IMAGE, &gpuMask_),
          "NvVFX_SetImage(GreenScreen output)");
    check(NvVFX_Load(green_), "NvVFX_Load(GreenScreen)");
    check(NvVFX_AllocateState(green_, &greenState_), "NvVFX_AllocateState(GreenScreen)");
    check(NvVFX_SetStateObjectHandleArray(green_, NVVFX_STATE, &greenState_),
          "NvVFX_SetStateObjectHandleArray(GreenScreen)");

    check(NvVFX_CreateEffect(NVVFX_FX_BGBLUR, &blur_), "NvVFX_CreateEffect(BackgroundBlur)");
    check(NvVFX_SetCudaStream(blur_, NVVFX_CUDA_STREAM, stream_),
          "NvVFX_SetCudaStream(BackgroundBlur)");
    check(NvVFX_SetF32(blur_, NVVFX_STRENGTH, opts_.blurStrength),
          "NvVFX_SetF32(BackgroundBlur Strength)");
    check(NvVFX_SetImage(blur_, NVVFX_INPUT_IMAGE, &gpuInput_),
          "NvVFX_SetImage(BackgroundBlur input)");
    check(NvVFX_SetImage(blur_, NVVFX_INPUT_IMAGE_1, &gpuMask_),
          "NvVFX_SetImage(BackgroundBlur mask)");
    check(NvVFX_SetImage(blur_, NVVFX_OUTPUT_IMAGE, &gpuBlur_),
          "NvVFX_SetImage(BackgroundBlur output)");
    check(NvVFX_Load(blur_), "NvVFX_Load(BackgroundBlur)");
  }

  void cleanup() {
    NvCVImage_Dealloc(&staging_);
    NvCVImage_Dealloc(&gpuBlur_);
    NvCVImage_Dealloc(&gpuMask_);
    NvCVImage_Dealloc(&gpuInput_);
    if (blur_) {
      NvVFX_DestroyEffect(blur_);
      blur_ = nullptr;
    }
    if (greenState_) {
      NvVFX_DeallocateState(green_, greenState_);
      greenState_ = nullptr;
    }
    if (green_) {
      NvVFX_DestroyEffect(green_);
      green_ = nullptr;
    }
    if (stream_) {
      NvVFX_CudaStreamDestroy(stream_);
      stream_ = nullptr;
    }
  }

  Options opts_;
  int width_ = 0;
  int height_ = 0;
  std::vector<unsigned char> input_;
  std::vector<unsigned char> mask_;
  std::vector<unsigned char> blurred_;
  CUstream stream_ = nullptr;
  NvVFX_Handle green_ = nullptr;
  NvVFX_Handle blur_ = nullptr;
  NvVFX_StateObjectHandle greenState_ = nullptr;
  NvCVImage cpuInput_{};
  NvCVImage cpuMask_{};
  NvCVImage cpuBlur_{};
  NvCVImage gpuInput_{};
  NvCVImage gpuMask_{};
  NvCVImage gpuBlur_{};
  NvCVImage staging_{};
};

void runGreenScreenAndBlur(const Options &opts, const ImageBGR &input,
                           const std::string &maskPath,
                           const std::string &blurPath) {
  MaxineProcessor processor(opts, input.width, input.height);
  processor.input() = input.pixels;
  processor.run();
  if (!maskPath.empty()) {
    writePGM(maskPath, processor.mask(), input.width, input.height);
  }
  if (!blurPath.empty()) {
    writePPMFromBGR(blurPath, processor.blurred(), input.width, input.height);
  }
}

Options optionsFromFlags(const std::map<std::string, std::string> &flags) {
  Options opts;
  opts.sdkPath = flag(flags, "sdk-path", opts.sdkPath);
  opts.modelDir = flag(flags, "model-dir", opts.modelDir);
  opts.input = flag(flags, "input");
  opts.mask = flag(flags, "mask");
  opts.blur = flag(flags, "blur");
  const std::string strength = flag(flags, "blur-strength");
  if (!strength.empty()) {
    opts.blurStrength = std::strtof(strength.c_str(), nullptr);
  }
  const std::string width = flag(flags, "width");
  if (!width.empty()) {
    opts.width = std::atoi(width.c_str());
  }
  const std::string height = flag(flags, "height");
  if (!height.empty()) {
    opts.height = std::atoi(height.c_str());
  }
  const std::string fps = flag(flags, "fps");
  if (!fps.empty()) {
    opts.fps = std::atoi(fps.c_str());
  }
  return opts;
}

void doctor(const Options &opts) {
  unsigned int version = 0;
  check(NvVFX_GetVersion(&version), "NvVFX_GetVersion");
  ImageBGR input = syntheticBGR();
  runGreenScreenAndBlur(opts, input, "", "");
  std::printf("sdk_version=%u.%u.%u\n", (version >> 24) & 0xff,
              (version >> 16) & 0xff, (version >> 8) & 0xff);
  std::printf("maxine_smoke_ok=true\n");
}

void testImage(const Options &opts) {
  if (opts.input.empty()) {
    fail("--input is required");
  }
  if (opts.mask.empty()) {
    fail("--mask is required");
  }
  if (opts.blur.empty()) {
    fail("--blur is required");
  }
  ImageBGR input = readPPMAsBGR(opts.input);
  runGreenScreenAndBlur(opts, input, opts.mask, opts.blur);
  std::printf("width=%d\n", input.width);
  std::printf("height=%d\n", input.height);
  std::printf("mask=%s\n", opts.mask.c_str());
  std::printf("blur=%s\n", opts.blur.c_str());
}

bool readExact(FILE *f, std::vector<unsigned char> &buf) {
  size_t got = 0;
  while (got < buf.size()) {
    size_t n = std::fread(buf.data() + got, 1, buf.size() - got, f);
    if (n == 0) {
      if (std::feof(f) && got == 0) {
        return false;
      }
      fail("short raw frame read");
    }
    got += n;
  }
  return true;
}

void writeExact(FILE *f, const std::vector<unsigned char> &buf) {
  size_t written = 0;
  while (written < buf.size()) {
    size_t n = std::fwrite(buf.data() + written, 1, buf.size() - written, f);
    if (n == 0) {
      fail("raw frame write failed");
    }
    written += n;
  }
  std::fflush(f);
}

void stream(const Options &opts) {
  if (opts.width < 512 || opts.height < 288) {
    std::fprintf(stderr, "error: --width/--height must be at least 512x288, got %dx%d\n",
                 opts.width, opts.height);
    std::exit(1);
  }
  MaxineProcessor processor(opts, opts.width, opts.height);
  while (readExact(stdin, processor.input())) {
    processor.run();
    writeExact(stdout, processor.blurred());
  }
}

} // namespace

int main(int argc, char **argv) {
  if (argc < 2) {
    std::fprintf(stderr, "usage: nv-vcam-maxine-helper doctor|test-image [flags]\n");
    return 2;
  }

  NvVFX_ConfigureLogger(NVCV_LOG_ERROR, "stderr", nullptr, nullptr);
  const std::string command = argv[1];
  Options opts = optionsFromFlags(parseFlags(argc, argv, 2));

  if (command == "doctor") {
    doctor(opts);
    return 0;
  }
  if (command == "test-image") {
    testImage(opts);
    return 0;
  }
  if (command == "stream") {
    stream(opts);
    return 0;
  }

  std::fprintf(stderr, "unknown command: %s\n", command.c_str());
  return 2;
}
