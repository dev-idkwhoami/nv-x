#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cerrno>
#include <algorithm>
#include <cctype>
#include <map>
#include <string>
#include <vector>

#include <fcntl.h>
#include <linux/videodev2.h>
#include <poll.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <unistd.h>

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
  std::string final;
  std::string inputDevice = "/dev/video0";
  std::string inputFormat = "nv12";
  std::string outputDevice = "/dev/video10";
  std::string outputFormat = "yuv420p";
  std::string idleLabel = "NV-X idling ...";
  std::string background = "blur";
  std::string replacement;
  std::string chromaColor = "#00ff00";
  int width = 0;
  int height = 0;
  int fps = 25;
  float blurStrength = 0.75f;
};

struct MMapBuffer {
  void *start = nullptr;
  size_t length = 0;
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

bool checkFrame(NvCV_Status status, const char *step) {
  if (status == NVCV_SUCCESS) {
    return true;
  }
  static int warningCount = 0;
  if (warningCount < 10) {
    std::fprintf(stderr, "warning: %s: %d (%s)\n", step, status,
                 NvCV_GetErrorStringFromCode(status));
  } else if (warningCount == 10) {
    std::fprintf(stderr, "warning: suppressing repeated Maxine frame errors\n");
  }
  ++warningCount;
  return false;
}

void checkSys(bool ok, const char *step) {
  if (ok) {
    return;
  }
  std::fprintf(stderr, "error: %s: %s\n", step, std::strerror(errno));
  std::exit(1);
}

int xioctl(int fd, unsigned long request, void *arg, const char *step) {
  int r = 0;
  do {
    r = ioctl(fd, request, arg);
  } while (r == -1 && errno == EINTR);
  if (r == -1) {
    checkSys(false, step);
  }
  return r;
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

bool parseBool(const std::string &value) {
  return value == "1" || value == "true" || value == "yes" || value == "on";
}

unsigned char hexByte(const std::string &value, size_t offset) {
  char buf[3] = {value[offset], value[offset + 1], '\0'};
  return static_cast<unsigned char>(std::strtoul(buf, nullptr, 16));
}

void parseChromaBGR(const std::string &value, unsigned char out[3]) {
  if (value.size() != 7 || value[0] != '#') {
    failf("invalid --chroma-color: ", value);
  }
  out[2] = hexByte(value, 1);
  out[1] = hexByte(value, 3);
  out[0] = hexByte(value, 5);
}

unsigned char clampByte(int value) {
  return static_cast<unsigned char>(std::max(0, std::min(255, value)));
}

void yuvToBGRPixel(int y, int u, int v, unsigned char *bgr) {
  const int c = y - 16;
  const int d = u - 128;
  const int e = v - 128;
  bgr[2] = clampByte((298 * c + 409 * e + 128) >> 8);
  bgr[1] = clampByte((298 * c - 100 * d - 208 * e + 128) >> 8);
  bgr[0] = clampByte((298 * c + 516 * d + 128) >> 8);
}

void nv12ToBGR(const unsigned char *src, int width, int height,
               std::vector<unsigned char> &bgr) {
  const unsigned char *yPlane = src;
  const unsigned char *uvPlane = src + static_cast<size_t>(width) * height;
  for (int y = 0; y < height; ++y) {
    for (int x = 0; x < width; ++x) {
      const int uv = (y / 2) * width + (x / 2) * 2;
      yuvToBGRPixel(yPlane[y * width + x], uvPlane[uv], uvPlane[uv + 1],
                    &bgr[(static_cast<size_t>(y) * width + x) * 3]);
    }
  }
}

void yu12ToBGR(const unsigned char *src, int width, int height,
               std::vector<unsigned char> &bgr) {
  const size_t ySize = static_cast<size_t>(width) * height;
  const size_t chromaSize = ySize / 4;
  const unsigned char *yPlane = src;
  const unsigned char *uPlane = src + ySize;
  const unsigned char *vPlane = uPlane + chromaSize;
  const int chromaWidth = width / 2;
  for (int y = 0; y < height; ++y) {
    for (int x = 0; x < width; ++x) {
      const int c = (y / 2) * chromaWidth + (x / 2);
      yuvToBGRPixel(yPlane[y * width + x], uPlane[c], vPlane[c],
                    &bgr[(static_cast<size_t>(y) * width + x) * 3]);
    }
  }
}

void bgrToYU12(const std::vector<unsigned char> &bgr, int width, int height,
               std::vector<unsigned char> &out) {
  const size_t ySize = static_cast<size_t>(width) * height;
  const size_t chromaSize = ySize / 4;
  out.assign(ySize + chromaSize * 2, 0);
  unsigned char *yPlane = out.data();
  unsigned char *uPlane = yPlane + ySize;
  unsigned char *vPlane = uPlane + chromaSize;
  const int chromaWidth = width / 2;

  for (int y = 0; y < height; ++y) {
    for (int x = 0; x < width; ++x) {
      const size_t i = (static_cast<size_t>(y) * width + x) * 3;
      const int b = bgr[i + 0];
      const int g = bgr[i + 1];
      const int r = bgr[i + 2];
      yPlane[y * width + x] = clampByte(((66 * r + 129 * g + 25 * b + 128) >> 8) + 16);
    }
  }

  for (int y = 0; y < height; y += 2) {
    for (int x = 0; x < width; x += 2) {
      int uSum = 0;
      int vSum = 0;
      for (int dy = 0; dy < 2; ++dy) {
        for (int dx = 0; dx < 2; ++dx) {
          const size_t i = (static_cast<size_t>(y + dy) * width + (x + dx)) * 3;
          const int b = bgr[i + 0];
          const int g = bgr[i + 1];
          const int r = bgr[i + 2];
          uSum += ((-38 * r - 74 * g + 112 * b + 128) >> 8) + 128;
          vSum += ((112 * r - 94 * g - 18 * b + 128) >> 8) + 128;
        }
      }
      const int c = (y / 2) * chromaWidth + (x / 2);
      uPlane[c] = clampByte(uSum / 4);
      vPlane[c] = clampByte(vSum / 4);
    }
  }
}

unsigned int fourccForFormat(const std::string &format) {
  std::string lowered = format;
  std::transform(lowered.begin(), lowered.end(), lowered.begin(),
                 [](unsigned char ch) { return static_cast<char>(std::tolower(ch)); });
  if (lowered == "nv12") {
    return V4L2_PIX_FMT_NV12;
  }
  if (lowered == "yuv420p" || lowered == "yu12") {
    return V4L2_PIX_FMT_YUV420;
  }
  failf("unsupported pixel format: ", format);
}

void maskToBGR(const std::vector<unsigned char> &mask, std::vector<unsigned char> &bgr) {
  for (size_t i = 0; i < mask.size(); ++i) {
    bgr[i * 3 + 0] = mask[i];
    bgr[i * 3 + 1] = mask[i];
    bgr[i * 3 + 2] = mask[i];
  }
}

void fillSolidBGR(const unsigned char color[3], size_t pixels, std::vector<unsigned char> &out) {
  for (size_t i = 0; i < pixels; ++i) {
    out[i * 3 + 0] = color[0];
    out[i * 3 + 1] = color[1];
    out[i * 3 + 2] = color[2];
  }
}

void compositeReplacement(const std::vector<unsigned char> &foreground,
                          const std::vector<unsigned char> &mask,
                          const std::vector<unsigned char> &replacement,
                          std::vector<unsigned char> &out) {
  for (size_t i = 0; i < mask.size(); ++i) {
    const unsigned int alpha = mask[i];
    const unsigned int invAlpha = 255 - alpha;
    out[i * 3 + 0] = static_cast<unsigned char>(
        (foreground[i * 3 + 0] * alpha + replacement[i * 3 + 0] * invAlpha) / 255);
    out[i * 3 + 1] = static_cast<unsigned char>(
        (foreground[i * 3 + 1] * alpha + replacement[i * 3 + 1] * invAlpha) / 255);
    out[i * 3 + 2] = static_cast<unsigned char>(
        (foreground[i * 3 + 2] * alpha + replacement[i * 3 + 2] * invAlpha) / 255);
  }
}

void resizeBGRNearest(const std::vector<unsigned char> &src, int srcWidth, int srcHeight,
                      int dstWidth, int dstHeight, std::vector<unsigned char> &dst) {
  if (srcWidth == dstWidth && srcHeight == dstHeight) {
    dst = src;
    return;
  }
  dst.resize(static_cast<size_t>(dstWidth) * dstHeight * 3);
  for (int y = 0; y < dstHeight; ++y) {
    const int srcY = std::min(srcHeight - 1, y * srcHeight / dstHeight);
    for (int x = 0; x < dstWidth; ++x) {
      const int srcX = std::min(srcWidth - 1, x * srcWidth / dstWidth);
      const size_t srcI = (static_cast<size_t>(srcY) * srcWidth + srcX) * 3;
      const size_t dstI = (static_cast<size_t>(y) * dstWidth + x) * 3;
      dst[dstI + 0] = src[srcI + 0];
      dst[dstI + 1] = src[srcI + 1];
      dst[dstI + 2] = src[srcI + 2];
    }
  }
}

struct FrameSize {
  int width = 0;
  int height = 0;
};

FrameSize effectFrameSize(int width, int height) {
  const int maxWidth = 960;
  const int maxHeight = 540;
  if (width <= maxWidth && height <= maxHeight) {
    return {width, height};
  }

  int scaledWidth = width;
  int scaledHeight = height;
  if (width * maxHeight > height * maxWidth) {
    scaledWidth = maxWidth;
    scaledHeight = height * maxWidth / width;
  } else {
    scaledHeight = maxHeight;
    scaledWidth = width * maxHeight / height;
  }
  scaledWidth = std::max(512, scaledWidth & ~1);
  scaledHeight = std::max(288, scaledHeight & ~1);
  return {scaledWidth, scaledHeight};
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
        blurred_(static_cast<size_t>(width) * height * 3),
        replacement_(static_cast<size_t>(width) * height * 3),
        final_(static_cast<size_t>(width) * height * 3) {
    if (opts_.background == "replace") {
      if (opts_.replacement.empty()) {
        fail("--replacement is required when --background replace");
      }
      ImageBGR bg = readPPMAsBGR(opts_.replacement);
      if (bg.width != width_ || bg.height != height_) {
        resizeBGRNearest(bg.pixels, bg.width, bg.height, width_, height_, replacement_);
      } else {
        replacement_ = bg.pixels;
      }
    } else if (opts_.background == "chroma") {
      unsigned char color[3] = {};
      parseChromaBGR(opts_.chromaColor, color);
      fillSolidBGR(color, static_cast<size_t>(width_) * height_, replacement_);
    }
    init();
  }

  ~MaxineProcessor() { cleanup(); }

  std::vector<unsigned char> &input() { return input_; }
  std::vector<unsigned char> &mask() { return mask_; }
  std::vector<unsigned char> &blurred() { return blurred_; }
  std::vector<unsigned char> &final() { return final_; }

  bool run() {
    NvCVImage *effectInput = &gpuInput_;
    if (!checkFrame(NvCVImage_Transfer(&cpuInput_, &gpuInput_, 1.0f, stream_, &staging_),
                    "NvCVImage_Transfer(input CPU->GPU)")) {
      return false;
    }
    if (!checkFrame(NvVFX_SetImage(green_, NVVFX_INPUT_IMAGE, effectInput),
                    "NvVFX_SetImage(GreenScreen input)") ||
        !checkFrame(NvVFX_Run(green_, 0), "NvVFX_Run(GreenScreen)") ||
        !checkFrame(NvVFX_CudaStreamSynchronize(stream_),
                    "NvVFX_CudaStreamSynchronize(GreenScreen)")) {
      return false;
    }
    if (usesBlur()) {
      if (!checkFrame(NvVFX_SetImage(blur_, NVVFX_INPUT_IMAGE, effectInput),
                      "NvVFX_SetImage(BackgroundBlur input)") ||
          !checkFrame(NvVFX_Run(blur_, 0), "NvVFX_Run(BackgroundBlur)") ||
          !checkFrame(NvVFX_CudaStreamSynchronize(stream_),
                      "NvVFX_CudaStreamSynchronize(BackgroundBlur)")) {
        return false;
      }
    }
    if (!checkFrame(NvCVImage_Transfer(&gpuMask_, &cpuMask_, 1.0f, stream_, &staging_),
                    "NvCVImage_Transfer(mask GPU->CPU)")) {
      return false;
    }
    if (usesBlur()) {
      if (!checkFrame(NvCVImage_Transfer(&gpuBlur_, &cpuBlur_, 1.0f, stream_, &staging_),
                      "NvCVImage_Transfer(blur GPU->CPU)")) {
        return false;
      }
    }
    if (!checkFrame(NvVFX_CudaStreamSynchronize(stream_),
                    "NvVFX_CudaStreamSynchronize(output copies)")) {
      return false;
    }
    if (opts_.background == "blur") {
      final_ = blurred_;
    } else if (opts_.background == "mask") {
      maskToBGR(mask_, final_);
    } else if (opts_.background == "chroma") {
      compositeReplacement(input_, mask_, replacement_, final_);
    } else if (opts_.background == "replace") {
      compositeReplacement(input_, mask_, replacement_, final_);
    } else {
      failf("unknown background mode: ", opts_.background);
    }
    return true;
  }

private:
  bool usesBlur() const { return opts_.background == "blur"; }

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
    if (usesBlur()) {
      check(NvCVImage_Init(&cpuBlur_, width_, height_, width_ * 3,
                           blurred_.data(), NVCV_BGR, NVCV_U8, NVCV_INTERLEAVED,
                           NVCV_CPU),
            "NvCVImage_Init(cpu blur)");
    }
    check(NvCVImage_Alloc(&gpuInput_, width_, height_, NVCV_BGR, NVCV_U8,
                          NVCV_INTERLEAVED, NVCV_GPU, 0),
          "NvCVImage_Alloc(gpu input)");
    check(NvCVImage_Alloc(&gpuMask_, width_, height_, NVCV_A, NVCV_U8,
                          NVCV_INTERLEAVED, NVCV_GPU, 0),
          "NvCVImage_Alloc(gpu mask)");
    if (usesBlur()) {
      check(NvCVImage_Alloc(&gpuBlur_, width_, height_, NVCV_BGR, NVCV_U8,
                            NVCV_INTERLEAVED, NVCV_GPU, 0),
            "NvCVImage_Alloc(gpu blur)");
    }
    check(NvVFX_SetImage(green_, NVVFX_INPUT_IMAGE, &gpuInput_),
          "NvVFX_SetImage(GreenScreen input)");
    check(NvVFX_SetImage(green_, NVVFX_OUTPUT_IMAGE, &gpuMask_),
          "NvVFX_SetImage(GreenScreen output)");
    check(NvVFX_Load(green_), "NvVFX_Load(GreenScreen)");
    check(NvVFX_AllocateState(green_, &greenState_), "NvVFX_AllocateState(GreenScreen)");
    check(NvVFX_SetStateObjectHandleArray(green_, NVVFX_STATE, &greenState_),
          "NvVFX_SetStateObjectHandleArray(GreenScreen)");

    if (usesBlur()) {
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
  std::vector<unsigned char> replacement_;
  std::vector<unsigned char> final_;
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

class TransferProcessor {
public:
  TransferProcessor(int width, int height)
      : width_(width), height_(height),
        bgr_(static_cast<size_t>(width) * height * 3) {
    init();
  }

  ~TransferProcessor() { cleanup(); }

  bool runNV12(const unsigned char *frame) {
    NvCVImage cpuNv12{};
    NvCV_Status status = NvCVImage_Init(&cpuNv12, width_, height_, width_,
                                        const_cast<unsigned char *>(frame),
                                        NVCV_YUV420, NVCV_U8, NVCV_NV12,
                                        NVCV_CPU);
    if (!checkFrame(status, "NvCVImage_Init(cpu NV12 input)")) {
      return false;
    }
    cpuNv12.colorspace = NVCV_709 | NVCV_VIDEO_RANGE | NVCV_CHROMA_COSITED;

    if (!checkFrame(NvCVImage_Transfer(&cpuNv12, &gpuBGR_, 1.0f, stream_, &staging_),
                    "NvCVImage_Transfer(NV12 CPU->BGR GPU)") ||
        !checkFrame(NvCVImage_Transfer(&gpuBGR_, &cpuBGR_, 1.0f, stream_, &staging_),
                    "NvCVImage_Transfer(BGR GPU->BGR CPU)") ||
        !checkFrame(NvVFX_CudaStreamSynchronize(stream_),
                    "NvVFX_CudaStreamSynchronize(transfer roundtrip)")) {
      return false;
    }
    return true;
  }

  const std::vector<unsigned char> &bgr() const { return bgr_; }

private:
  void init() {
    check(NvVFX_CudaStreamCreate(&stream_), "NvVFX_CudaStreamCreate");
    check(NvCVImage_Init(&cpuBGR_, width_, height_, width_ * 3,
                         bgr_.data(), NVCV_BGR, NVCV_U8, NVCV_INTERLEAVED,
                         NVCV_CPU),
          "NvCVImage_Init(cpu BGR output)");
    check(NvCVImage_Alloc(&gpuBGR_, width_, height_, NVCV_BGR, NVCV_U8,
                          NVCV_INTERLEAVED, NVCV_GPU, 0),
          "NvCVImage_Alloc(gpu BGR transfer)");
  }

  void cleanup() {
    NvCVImage_Dealloc(&staging_);
    NvCVImage_Dealloc(&gpuBGR_);
    if (stream_) {
      NvVFX_CudaStreamDestroy(stream_);
      stream_ = nullptr;
    }
  }

  int width_ = 0;
  int height_ = 0;
  std::vector<unsigned char> bgr_;
  CUstream stream_ = nullptr;
  NvCVImage cpuBGR_{};
  NvCVImage gpuBGR_{};
  NvCVImage staging_{};
};

void runGreenScreenAndBlur(const Options &opts, const ImageBGR &input,
                           const std::string &maskPath,
                           const std::string &blurPath) {
  MaxineProcessor processor(opts, input.width, input.height);
  processor.input() = input.pixels;
  if (!processor.run()) {
    fail("Maxine processing failed");
  }
  if (!maskPath.empty()) {
    writePGM(maskPath, processor.mask(), input.width, input.height);
  }
  if (!blurPath.empty()) {
    writePPMFromBGR(blurPath, processor.blurred(), input.width, input.height);
  }
  if (!opts.final.empty()) {
    writePPMFromBGR(opts.final, processor.final(), input.width, input.height);
  }
}

Options optionsFromFlags(const std::map<std::string, std::string> &flags) {
  Options opts;
  opts.sdkPath = flag(flags, "sdk-path", opts.sdkPath);
  opts.modelDir = flag(flags, "model-dir", opts.modelDir);
  opts.input = flag(flags, "input");
  opts.mask = flag(flags, "mask");
  opts.blur = flag(flags, "blur");
  opts.final = flag(flags, "final");
  opts.inputDevice = flag(flags, "input-device", opts.inputDevice);
  opts.inputFormat = flag(flags, "input-format", opts.inputFormat);
  opts.outputDevice = flag(flags, "output-device", opts.outputDevice);
  opts.outputFormat = flag(flags, "output-format", opts.outputFormat);
  opts.idleLabel = flag(flags, "idle-label", opts.idleLabel);
  opts.background = flag(flags, "background", opts.background);
  opts.replacement = flag(flags, "replacement");
  opts.chromaColor = flag(flags, "chroma-color", opts.chromaColor);
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
  if (opts.final.empty()) {
    fail("--final is required");
  }
  ImageBGR input = readPPMAsBGR(opts.input);
  runGreenScreenAndBlur(opts, input, opts.mask, opts.blur);
  std::printf("width=%d\n", input.width);
  std::printf("height=%d\n", input.height);
  std::printf("mask=%s\n", opts.mask.c_str());
  std::printf("blur=%s\n", opts.blur.c_str());
  std::printf("final=%s\n", opts.final.c_str());
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
    if (!processor.run()) {
      fail("Maxine processing failed");
    }
    writeExact(stdout, processor.final());
  }
}

std::vector<MMapBuffer> setupInputBuffers(int fd) {
  v4l2_requestbuffers req{};
  req.count = 4;
  req.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
  req.memory = V4L2_MEMORY_MMAP;
  xioctl(fd, VIDIOC_REQBUFS, &req, "VIDIOC_REQBUFS");
  if (req.count < 2) {
    fail("V4L2 input returned too few mmap buffers");
  }

  std::vector<MMapBuffer> buffers(req.count);
  for (unsigned int i = 0; i < req.count; ++i) {
    v4l2_buffer buf{};
    buf.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    buf.memory = V4L2_MEMORY_MMAP;
    buf.index = i;
    xioctl(fd, VIDIOC_QUERYBUF, &buf, "VIDIOC_QUERYBUF");
    buffers[i].length = buf.length;
    buffers[i].start = mmap(nullptr, buf.length, PROT_READ | PROT_WRITE, MAP_SHARED,
                            fd, buf.m.offset);
    checkSys(buffers[i].start != MAP_FAILED, "mmap input buffer");
  }
  return buffers;
}

void queueAllInputBuffers(int fd, const std::vector<MMapBuffer> &buffers) {
  for (unsigned int i = 0; i < buffers.size(); ++i) {
    v4l2_buffer buf{};
    buf.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    buf.memory = V4L2_MEMORY_MMAP;
    buf.index = i;
    xioctl(fd, VIDIOC_QBUF, &buf, "VIDIOC_QBUF");
  }
}

void setCaptureFormat(int fd, const Options &opts) {
  v4l2_format fmt{};
  fmt.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
  fmt.fmt.pix.width = opts.width;
  fmt.fmt.pix.height = opts.height;
  fmt.fmt.pix.pixelformat = fourccForFormat(opts.inputFormat);
  fmt.fmt.pix.field = V4L2_FIELD_NONE;
  xioctl(fd, VIDIOC_S_FMT, &fmt, "VIDIOC_S_FMT(input)");
  if (static_cast<int>(fmt.fmt.pix.width) != opts.width ||
      static_cast<int>(fmt.fmt.pix.height) != opts.height ||
      fmt.fmt.pix.pixelformat != fourccForFormat(opts.inputFormat)) {
    fail("input device did not accept requested format");
  }

  v4l2_streamparm parm{};
  parm.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
  parm.parm.capture.timeperframe.numerator = 1;
  parm.parm.capture.timeperframe.denominator = opts.fps;
  xioctl(fd, VIDIOC_S_PARM, &parm, "VIDIOC_S_PARM(input)");
}

void setOutputFormat(int fd, const Options &opts) {
  v4l2_format fmt{};
  fmt.type = V4L2_BUF_TYPE_VIDEO_OUTPUT;
  fmt.fmt.pix.width = opts.width;
  fmt.fmt.pix.height = opts.height;
  fmt.fmt.pix.pixelformat = fourccForFormat(opts.outputFormat);
  fmt.fmt.pix.field = V4L2_FIELD_NONE;
  fmt.fmt.pix.bytesperline = opts.width;
  fmt.fmt.pix.sizeimage = static_cast<unsigned int>(opts.width * opts.height * 3 / 2);
  xioctl(fd, VIDIOC_S_FMT, &fmt, "VIDIOC_S_FMT(output)");
  if (static_cast<int>(fmt.fmt.pix.width) != opts.width ||
      static_cast<int>(fmt.fmt.pix.height) != opts.height ||
      fmt.fmt.pix.pixelformat != fourccForFormat(opts.outputFormat)) {
    fail("output device did not accept requested format");
  }

  v4l2_streamparm parm{};
  parm.type = V4L2_BUF_TYPE_VIDEO_OUTPUT;
  parm.parm.output.timeperframe.numerator = 1;
  parm.parm.output.timeperframe.denominator = opts.fps;
  xioctl(fd, VIDIOC_S_PARM, &parm, "VIDIOC_S_PARM(output)");
}

void writeAllFD(int fd, const std::vector<unsigned char> &buf) {
  size_t written = 0;
  while (written < buf.size()) {
    ssize_t n = write(fd, buf.data() + written, buf.size() - written);
    if (n < 0 && errno == EINTR) {
      continue;
    }
    checkSys(n > 0, "write output frame");
    written += static_cast<size_t>(n);
  }
}

void validateNativeIOOptions(const Options &opts) {
  if (opts.width < 512 || opts.height < 288) {
    std::fprintf(stderr, "error: --width/--height must be at least 512x288, got %dx%d\n",
                 opts.width, opts.height);
    std::exit(1);
  }
  if ((opts.width % 2) != 0 || (opts.height % 2) != 0) {
    fail("--width and --height must be even for NV12/YU12");
  }
  if (fourccForFormat(opts.outputFormat) != V4L2_PIX_FMT_YUV420) {
    fail("--output-format must be yuv420p/yu12");
  }
}

void nativeStream(const Options &opts) {
  validateNativeIOOptions(opts);
  int inputFD = open(opts.inputDevice.c_str(), O_RDWR | O_NONBLOCK);
  checkSys(inputFD >= 0, "open input device");
  int outputFD = open(opts.outputDevice.c_str(), O_WRONLY);
  checkSys(outputFD >= 0, "open output device");

  setCaptureFormat(inputFD, opts);
  setOutputFormat(outputFD, opts);
  std::vector<MMapBuffer> buffers = setupInputBuffers(inputFD);
  queueAllInputBuffers(inputFD, buffers);

  int type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
  xioctl(inputFD, VIDIOC_STREAMON, &type, "VIDIOC_STREAMON");

  const FrameSize fxSize = effectFrameSize(opts.width, opts.height);
  if (fxSize.width != opts.width || fxSize.height != opts.height) {
    std::fprintf(stderr, "info: native-stream processing FX at %dx%d for %dx%d output\n",
                 fxSize.width, fxSize.height, opts.width, opts.height);
  }
  TransferProcessor transfer(opts.width, opts.height);
  MaxineProcessor processor(opts, fxSize.width, fxSize.height);
  std::vector<unsigned char> inputBGR(static_cast<size_t>(opts.width) * opts.height * 3);
  std::vector<unsigned char> outputBGR(static_cast<size_t>(opts.width) * opts.height * 3);
  std::vector<unsigned char> yu12;
  const size_t expectedFrameSize = static_cast<size_t>(opts.width) * opts.height * 3 / 2;
  bool effectsDisabled = false;

  for (;;) {
    pollfd pfd{};
    pfd.fd = inputFD;
    pfd.events = POLLIN;
    int pr = poll(&pfd, 1, 2000);
    if (pr < 0 && errno == EINTR) {
      continue;
    }
    checkSys(pr > 0, "poll input device");

    v4l2_buffer buf{};
    buf.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    buf.memory = V4L2_MEMORY_MMAP;
    xioctl(inputFD, VIDIOC_DQBUF, &buf, "VIDIOC_DQBUF");
    if (buf.index >= buffers.size() || buffers[buf.index].length < expectedFrameSize) {
      fail("input buffer is smaller than expected");
    }

    const unsigned char *frame = static_cast<const unsigned char *>(buffers[buf.index].start);
    const std::vector<unsigned char> *sourceBGR = &inputBGR;
    if (fourccForFormat(opts.inputFormat) == V4L2_PIX_FMT_NV12) {
      if (!transfer.runNV12(frame)) {
        fail("native stream transfer failed");
      }
      sourceBGR = &transfer.bgr();
    } else {
      yu12ToBGR(frame, opts.width, opts.height, inputBGR);
    }
    resizeBGRNearest(*sourceBGR, opts.width, opts.height, fxSize.width, fxSize.height,
                     processor.input());
    if (effectsDisabled) {
      resizeBGRNearest(processor.input(), fxSize.width, fxSize.height, opts.width, opts.height,
                       outputBGR);
    } else if (!processor.run()) {
      std::fprintf(stderr, "warning: Maxine failed; using passthrough for this stream\n");
      effectsDisabled = true;
      resizeBGRNearest(processor.input(), fxSize.width, fxSize.height, opts.width, opts.height,
                       outputBGR);
    } else {
      resizeBGRNearest(processor.final(), fxSize.width, fxSize.height, opts.width, opts.height,
                       outputBGR);
    }
    bgrToYU12(outputBGR, opts.width, opts.height, yu12);
    writeAllFD(outputFD, yu12);
    xioctl(inputFD, VIDIOC_QBUF, &buf, "VIDIOC_QBUF");
  }

  xioctl(inputFD, VIDIOC_STREAMOFF, &type, "VIDIOC_STREAMOFF");
  for (const auto &buffer : buffers) {
    if (buffer.start && buffer.start != MAP_FAILED) {
      munmap(buffer.start, buffer.length);
    }
  }
  close(outputFD);
  close(inputFD);
}

void nativeTransfer(const Options &opts) {
  validateNativeIOOptions(opts);
  if (fourccForFormat(opts.inputFormat) != V4L2_PIX_FMT_NV12) {
    fail("--input-format must be nv12 for native-transfer");
  }

  int inputFD = open(opts.inputDevice.c_str(), O_RDWR | O_NONBLOCK);
  checkSys(inputFD >= 0, "open input device");
  int outputFD = open(opts.outputDevice.c_str(), O_WRONLY);
  checkSys(outputFD >= 0, "open output device");

  setCaptureFormat(inputFD, opts);
  setOutputFormat(outputFD, opts);
  std::vector<MMapBuffer> buffers = setupInputBuffers(inputFD);
  queueAllInputBuffers(inputFD, buffers);

  int type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
  xioctl(inputFD, VIDIOC_STREAMON, &type, "VIDIOC_STREAMON");

  TransferProcessor processor(opts.width, opts.height);
  std::vector<unsigned char> yu12;
  const size_t expectedFrameSize = static_cast<size_t>(opts.width) * opts.height * 3 / 2;

  for (;;) {
    pollfd pfd{};
    pfd.fd = inputFD;
    pfd.events = POLLIN;
    int pr = poll(&pfd, 1, 2000);
    if (pr < 0 && errno == EINTR) {
      continue;
    }
    checkSys(pr > 0, "poll input device");

    v4l2_buffer buf{};
    buf.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    buf.memory = V4L2_MEMORY_MMAP;
    xioctl(inputFD, VIDIOC_DQBUF, &buf, "VIDIOC_DQBUF");
    if (buf.index >= buffers.size() || buffers[buf.index].length < expectedFrameSize) {
      fail("input buffer is smaller than expected");
    }

    const unsigned char *frame = static_cast<const unsigned char *>(buffers[buf.index].start);
    if (!processor.runNV12(frame)) {
      fail("native transfer roundtrip failed");
    }
    bgrToYU12(processor.bgr(), opts.width, opts.height, yu12);
    writeAllFD(outputFD, yu12);
    xioctl(inputFD, VIDIOC_QBUF, &buf, "VIDIOC_QBUF");
  }

  xioctl(inputFD, VIDIOC_STREAMOFF, &type, "VIDIOC_STREAMOFF");
  for (const auto &buffer : buffers) {
    if (buffer.start && buffer.start != MAP_FAILED) {
      munmap(buffer.start, buffer.length);
    }
  }
  close(outputFD);
  close(inputFD);
}

std::vector<unsigned char> blackYU12Frame(int width, int height) {
  const size_t ySize = static_cast<size_t>(width) * height;
  const size_t chromaSize = ySize / 4;
  std::vector<unsigned char> frame(ySize + chromaSize * 2);
  std::fill(frame.begin(), frame.begin() + ySize, 16);
  std::fill(frame.begin() + ySize, frame.end(), 128);
  return frame;
}

const char *glyphRows(char ch) {
  switch (static_cast<char>(std::toupper(static_cast<unsigned char>(ch)))) {
  case 'A': return "01110""10001""10001""11111""10001""10001""10001";
  case 'C': return "01111""10000""10000""10000""10000""10000""01111";
  case 'D': return "11110""10001""10001""10001""10001""10001""11110";
  case 'G': return "01111""10000""10000""10111""10001""10001""01111";
  case 'I': return "11111""00100""00100""00100""00100""00100""11111";
  case 'L': return "10000""10000""10000""10000""10000""10000""11111";
  case 'M': return "10001""11011""10101""10101""10001""10001""10001";
  case 'N': return "10001""11001""10101""10011""10001""10001""10001";
  case 'V': return "10001""10001""10001""10001""10001""01010""00100";
  case '-': return "00000""00000""00000""11111""00000""00000""00000";
  case '.': return "00000""00000""00000""00000""00000""01100""01100";
  case ' ': return "00000""00000""00000""00000""00000""00000""00000";
  default: return "11111""10001""00010""00100""00100""00000""00100";
  }
}

void drawIdleLabel(std::vector<unsigned char> &frame, int width, int height,
                   const std::string &label) {
  if (label.empty()) {
    return;
  }
  unsigned char *yPlane = frame.data();
  const int scale = std::max(4, std::min(width / 180, height / 90));
  const int glyphWidth = 5 * scale;
  const int glyphHeight = 7 * scale;
  const int spacing = scale * 2;
  const int textWidth = static_cast<int>(label.size()) * glyphWidth +
                        static_cast<int>(label.size() - 1) * spacing;
  int startX = (width - textWidth) / 2;
  int startY = (height - glyphHeight) / 2;
  startX = std::max(0, startX);
  startY = std::max(0, startY);

  for (size_t i = 0; i < label.size(); ++i) {
    const char *glyph = glyphRows(label[i]);
    const int glyphX = startX + static_cast<int>(i) * (glyphWidth + spacing);
    for (int gy = 0; gy < 7; ++gy) {
      for (int gx = 0; gx < 5; ++gx) {
        if (glyph[gy * 5 + gx] != '1') {
          continue;
        }
        for (int sy = 0; sy < scale; ++sy) {
          const int py = startY + gy * scale + sy;
          if (py < 0 || py >= height) {
            continue;
          }
          for (int sx = 0; sx < scale; ++sx) {
            const int px = glyphX + gx * scale + sx;
            if (px >= 0 && px < width) {
              yPlane[py * width + px] = 235;
            }
          }
        }
      }
    }
  }
}

void idleOutput(const Options &opts) {
  if (opts.width < 512 || opts.height < 288) {
    std::fprintf(stderr, "error: --width/--height must be at least 512x288, got %dx%d\n",
                 opts.width, opts.height);
    std::exit(1);
  }
  if ((opts.width % 2) != 0 || (opts.height % 2) != 0) {
    fail("--width and --height must be even for YU12");
  }
  if (fourccForFormat(opts.outputFormat) != V4L2_PIX_FMT_YUV420) {
    fail("--output-format must be yuv420p/yu12");
  }
  int outputFD = open(opts.outputDevice.c_str(), O_WRONLY);
  checkSys(outputFD >= 0, "open output device");
  setOutputFormat(outputFD, opts);

  std::vector<unsigned char> frame = blackYU12Frame(opts.width, opts.height);
  drawIdleLabel(frame, opts.width, opts.height, opts.idleLabel);
  const useconds_t delay = opts.fps > 0 ? static_cast<useconds_t>(1000000 / opts.fps) : 20000;
  for (;;) {
    writeAllFD(outputFD, frame);
    usleep(delay);
  }
  close(outputFD);
}

} // namespace

int main(int argc, char **argv) {
  if (argc < 2) {
    std::fprintf(stderr, "usage: nv-x-video doctor|test-image|stream|native-stream|native-transfer|idle-output [flags]\n");
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
  if (command == "native-stream") {
    nativeStream(opts);
    return 0;
  }
  if (command == "native-transfer") {
    nativeTransfer(opts);
    return 0;
  }
  if (command == "idle-output") {
    idleOutput(opts);
    return 0;
  }

  std::fprintf(stderr, "unknown command: %s\n", command.c_str());
  return 2;
}
