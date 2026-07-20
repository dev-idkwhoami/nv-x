#include <pipewire/pipewire.h>
#include <pipewire/link.h>
#include <spa/param/audio/format-utils.h>
#include <spa/param/props.h>

#include <nvAudioEffects.h>
#include <dereverb_denoiser.h>
#include <studio_voice_low_latency.h>

#include <algorithm>
#include <array>
#include <atomic>
#include <chrono>
#include <csignal>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <stdexcept>
#include <string>
#include <thread>
#include <unordered_set>

namespace {
constexpr uint32_t kRate = 48000;
constexpr uint32_t kChannels = 1;
constexpr uint32_t kFrameSamples = 480;
constexpr size_t kRingSamples = kRate * 2;

struct Options {
  std::string command;
  std::string mode = "dereverb_denoiser";
  std::string model;
  std::string inputNode;
  std::string outputNode = "nv-x-microphone";
  std::string outputDescription = "NV-X Microphone";
  bool monitor = false;
  std::string monitorOutputNode;
  float intensity = 0.90f;
};

void failStatus(NvAFX_Status status, const char *what) {
  if (status != NVAFX_STATUS_SUCCESS) {
    throw std::runtime_error(std::string(what) + " failed with NvAFX status " + std::to_string(status));
  }
}

Options parse(int argc, char **argv) {
  if (argc < 2) throw std::runtime_error("expected doctor or run");
  Options o;
  o.command = argv[1];
  for (int i = 2; i < argc; ++i) {
    std::string key = argv[i];
    if (i + 1 >= argc) throw std::runtime_error("missing value for " + key);
    std::string value = argv[++i];
    if (key == "--mode") o.mode = value;
    else if (key == "--model") o.model = value;
    else if (key == "--input-node") o.inputNode = value;
    else if (key == "--output-node") o.outputNode = value;
    else if (key == "--output-description") o.outputDescription = value;
    else if (key == "--monitor") o.monitor = value == "true";
    else if (key == "--monitor-output-node") o.monitorOutputNode = value;
    else if (key == "--intensity") o.intensity = std::stof(value);
    else if (key == "--sdk-path") { /* library resolution is handled by the launcher */ }
    else throw std::runtime_error("unknown option " + key);
  }
  if (o.model.empty()) throw std::runtime_error("--model is required");
  if (o.intensity < 0.0f || o.intensity > 1.0f) throw std::runtime_error("--intensity must be between 0 and 1");
  return o;
}

class Effect {
 public:
  explicit Effect(const Options &o) {
    const char *selector = nullptr;
    if (o.mode == "dereverb_denoiser") selector = NVAFX_EFFECT_DEREVERB_DENOISER;
    else if (o.mode == "studio_voice_low_latency") selector = NVAFX_EFFECT_STUDIO_VOICE_LOW_LATENCY;
    else throw std::runtime_error("unsupported mode " + o.mode);
    failStatus(NvAFX_CreateEffect(selector, &handle_), "NvAFX_CreateEffect");
    try {
      failStatus(NvAFX_SetU32(handle_, NVAFX_PARAM_INPUT_SAMPLE_RATE, kRate), "set sample rate");
      const char *model = o.model.c_str();
      failStatus(NvAFX_SetStringList(handle_, NVAFX_PARAM_MODEL_PATH, &model, 1), "set model");
      failStatus(NvAFX_SetU32(handle_, NVAFX_PARAM_NUM_STREAMS, 1), "set streams");
      failStatus(NvAFX_SetU32(handle_, NVAFX_PARAM_NUM_SAMPLES_PER_INPUT_FRAME, kFrameSamples), "set frame size");
      failStatus(NvAFX_Load(handle_), "NvAFX_Load");
      if (o.mode == "dereverb_denoiser") {
        auto status = NvAFX_SetFloat(handle_, NVAFX_PARAM_INTENSITY_RATIO, o.intensity);
        if (status != NVAFX_STATUS_SUCCESS && status != NVAFX_STATUS_INVALID_PARAM)
          failStatus(status, "set intensity");
      }
    } catch (...) {
      NvAFX_DestroyEffect(handle_);
      handle_ = nullptr;
      throw;
    }
  }
  ~Effect() { if (handle_) NvAFX_DestroyEffect(handle_); }
  void run(const float *input, float *output) {
    const float *inputs[] = {input};
    float *outputs[] = {output};
    failStatus(NvAFX_Run(handle_, inputs, outputs, kFrameSamples, kChannels), "NvAFX_Run");
  }
 private:
  NvAFX_Handle handle_ = nullptr;
};

class Ring {
 public:
  size_t push(const float *src, size_t count) {
    const size_t read = read_.load(std::memory_order_acquire);
    const size_t write = write_.load(std::memory_order_relaxed);
    const size_t free = kRingSamples - (write - read);
    count = std::min(count, free);
    for (size_t i = 0; i < count; ++i) data_[(write + i) % kRingSamples] = src[i];
    write_.store(write + count, std::memory_order_release);
    return count;
  }
  size_t pop(float *dst, size_t count) {
    const size_t write = write_.load(std::memory_order_acquire);
    const size_t read = read_.load(std::memory_order_relaxed);
    count = std::min(count, write - read);
    for (size_t i = 0; i < count; ++i) dst[i] = data_[(read + i) % kRingSamples];
    read_.store(read + count, std::memory_order_release);
    return count;
  }
  size_t size() const {
    return write_.load(std::memory_order_acquire) - read_.load(std::memory_order_acquire);
  }
  void discard() {
    read_.store(write_.load(std::memory_order_acquire), std::memory_order_release);
  }
 private:
  std::array<float, kRingSamples> data_{};
  std::atomic<size_t> read_{0}, write_{0};
};

struct Runtime {
  pw_main_loop *loop = nullptr;
  pw_stream *capture = nullptr;
  pw_stream *source = nullptr;
  pw_stream *monitor = nullptr;
  pw_registry *registry = nullptr;
  spa_hook registryListener{};
  Ring input, output, monitorOutput;
  std::atomic<bool> running{true};
  std::atomic<bool> consumerActive{false};
  std::atomic<bool> virtualConsumerActive{false};
  std::atomic<bool> monitorActive{false};
  std::atomic<bool> resetRequested{false};
  std::atomic<uint64_t> overflows{0}, underflows{0};
  bool captureConnected = false;
  std::unordered_set<uint32_t> consumerLinks;
  Effect *effect = nullptr;
};

void connectStream(pw_stream *stream, pw_direction direction);

void captureProcess(void *userdata) {
  auto *r = static_cast<Runtime *>(userdata);
  pw_buffer *pb = pw_stream_dequeue_buffer(r->capture);
  if (!pb) return;
  spa_buffer *b = pb->buffer;
  if (b->n_datas > 0 && b->datas[0].data) {
    auto *src = static_cast<float *>(b->datas[0].data);
    const auto &chunk = *b->datas[0].chunk;
    const size_t count = chunk.size / sizeof(float);
    if (r->input.push(src + chunk.offset / sizeof(float), count) != count) r->overflows++;
  }
  pw_stream_queue_buffer(r->capture, pb);
}

void sourceProcess(void *userdata) {
  auto *r = static_cast<Runtime *>(userdata);
  pw_buffer *pb = pw_stream_dequeue_buffer(r->source);
  if (!pb) return;
  spa_buffer *b = pb->buffer;
  if (b->n_datas == 0 || !b->datas[0].data) { pw_stream_queue_buffer(r->source, pb); return; }
  auto *dst = static_cast<float *>(b->datas[0].data);
  size_t count = b->datas[0].maxsize / sizeof(float);
  if (pb->requested) count = std::min(count, static_cast<size_t>(pb->requested));
  const size_t got = r->output.pop(dst, count);
  if (got < count) {
    std::fill(dst + got, dst + count, 0.0f);
    r->underflows++;
  }
  b->datas[0].chunk->offset = 0;
  b->datas[0].chunk->stride = sizeof(float);
  b->datas[0].chunk->size = count * sizeof(float);
  pw_stream_queue_buffer(r->source, pb);
}

void monitorProcess(void *userdata) {
  auto *r = static_cast<Runtime *>(userdata);
  pw_buffer *pb = pw_stream_dequeue_buffer(r->monitor);
  if (!pb) return;
  spa_buffer *b = pb->buffer;
  if (b->n_datas == 0 || !b->datas[0].data) { pw_stream_queue_buffer(r->monitor, pb); return; }
  auto *dst = static_cast<float *>(b->datas[0].data);
  size_t count = b->datas[0].maxsize / sizeof(float);
  if (pb->requested) count = std::min(count, static_cast<size_t>(pb->requested));
  const size_t got = r->monitorOutput.pop(dst, count);
  if (got < count) std::fill(dst + got, dst + count, 0.0f);
  b->datas[0].chunk->offset = 0;
  b->datas[0].chunk->stride = sizeof(float);
  b->datas[0].chunk->size = count * sizeof(float);
  pw_stream_queue_buffer(r->monitor, pb);
}

void setConsumerActive(Runtime *r, bool active) {
  if (r->consumerActive.exchange(active) == active) return;
  r->resetRequested = true;

  if (active && !r->captureConnected) {
    try {
      connectStream(r->capture, PW_DIRECTION_INPUT);
      r->captureConnected = true;
      std::fprintf(stdout, "audio_capture=active\n");
      std::fflush(stdout);
    } catch (const std::exception &e) {
      std::fprintf(stderr, "microphone activation failed: %s\n", e.what());
      r->running = false;
      pw_main_loop_quit(r->loop);
    }
  } else if (!active && r->captureConnected) {
    const int rc = pw_stream_disconnect(r->capture);
    if (rc < 0) std::fprintf(stderr, "microphone deactivation failed: %d\n", rc);
    r->captureConnected = false;
    std::fprintf(stdout, "audio_capture=inactive\n");
    std::fflush(stdout);
  }
}

void updateCaptureDemand(Runtime *r) {
  setConsumerActive(r, r->virtualConsumerActive.load() || r->monitorActive.load());
}

void registryGlobal(void *userdata, uint32_t id, uint32_t, const char *type, uint32_t,
                    const spa_dict *props) {
  auto *r = static_cast<Runtime *>(userdata);
  if (!type || std::strcmp(type, PW_TYPE_INTERFACE_Link) != 0 || !props) return;
  const char *outputNode = spa_dict_lookup(props, PW_KEY_LINK_OUTPUT_NODE);
  if (!outputNode) return;
  char *end = nullptr;
  const unsigned long node = std::strtoul(outputNode, &end, 10);
  if (!end || *end != '\0' || node != pw_stream_get_node_id(r->source)) return;
  r->consumerLinks.insert(id);
  r->virtualConsumerActive = true;
  updateCaptureDemand(r);
}

void registryGlobalRemove(void *userdata, uint32_t id) {
  auto *r = static_cast<Runtime *>(userdata);
  if (r->consumerLinks.erase(id) == 0) return;
  r->virtualConsumerActive = !r->consumerLinks.empty();
  updateCaptureDemand(r);
}

void monitorStateChanged(void *userdata, pw_stream_state, pw_stream_state state, const char *error) {
  auto *r = static_cast<Runtime *>(userdata);
  if (state == PW_STREAM_STATE_ERROR) {
    std::fprintf(stderr, "self hearing stream failed: %s\n", error ? error : "unknown error");
  }
  r->monitorActive = state == PW_STREAM_STATE_STREAMING;
  updateCaptureDemand(r);
}

void quit(void *userdata, int) {
  auto *r = static_cast<Runtime *>(userdata);
  r->running = false;
  pw_main_loop_quit(r->loop);
}

const pw_stream_events captureEvents = {.version = PW_VERSION_STREAM_EVENTS, .process = captureProcess};
const pw_stream_events sourceEvents = {
    .version = PW_VERSION_STREAM_EVENTS,
    .process = sourceProcess,
};
const pw_stream_events monitorEvents = {
    .version = PW_VERSION_STREAM_EVENTS,
    .state_changed = monitorStateChanged,
    .process = monitorProcess,
};
const pw_registry_events registryEvents = {
    .version = PW_VERSION_REGISTRY_EVENTS,
    .global = registryGlobal,
    .global_remove = registryGlobalRemove,
};

void connectStream(pw_stream *stream, pw_direction direction) {
  uint8_t buffer[1024];
  spa_pod_builder builder = SPA_POD_BUILDER_INIT(buffer, sizeof(buffer));
  const spa_pod *params[1];
  spa_audio_info_raw info = SPA_AUDIO_INFO_RAW_INIT(
      .format = SPA_AUDIO_FORMAT_F32,
      .rate = kRate,
      .channels = kChannels,
      .position = { SPA_AUDIO_CHANNEL_MONO });
  params[0] = spa_format_audio_raw_build(&builder, SPA_PARAM_EnumFormat, &info);
  int rc = pw_stream_connect(stream, direction, PW_ID_ANY,
      static_cast<pw_stream_flags>(PW_STREAM_FLAG_AUTOCONNECT | PW_STREAM_FLAG_MAP_BUFFERS | PW_STREAM_FLAG_RT_PROCESS),
      params, 1);
  if (rc < 0) throw std::runtime_error("pw_stream_connect failed: " + std::to_string(rc));
}

void runLive(const Options &o) {
  Effect effect(o);
  Runtime r;
  r.effect = &effect;
  pw_init(nullptr, nullptr);
  r.loop = pw_main_loop_new(nullptr);
  if (!r.loop) throw std::runtime_error("pw_main_loop_new failed");
  pw_loop_add_signal(pw_main_loop_get_loop(r.loop), SIGINT, quit, &r);
  pw_loop_add_signal(pw_main_loop_get_loop(r.loop), SIGTERM, quit, &r);

  pw_properties *captureProps = pw_properties_new(
      PW_KEY_MEDIA_TYPE, "Audio", PW_KEY_MEDIA_CATEGORY, "Capture",
      PW_KEY_MEDIA_ROLE, "Communication", PW_KEY_NODE_NAME, "nv-x-audio-capture", nullptr);
  if (!o.inputNode.empty()) pw_properties_set(captureProps, PW_KEY_TARGET_OBJECT, o.inputNode.c_str());
  r.capture = pw_stream_new_simple(pw_main_loop_get_loop(r.loop), "NV-X Audio Capture", captureProps, &captureEvents, &r);

  pw_properties *sourceProps = pw_properties_new(
      PW_KEY_MEDIA_TYPE, "Audio", PW_KEY_MEDIA_CATEGORY, "Playback", PW_KEY_MEDIA_ROLE, "Communication",
      PW_KEY_MEDIA_CLASS, "Audio/Source", PW_KEY_NODE_NAME, o.outputNode.c_str(),
      PW_KEY_NODE_DESCRIPTION, o.outputDescription.c_str(), PW_KEY_NODE_VIRTUAL, "true", nullptr);
  pw_properties_set(sourceProps, PW_KEY_NODE_PAUSE_ON_IDLE, "true");
  pw_properties_set(sourceProps, PW_KEY_NODE_PASSIVE, "true");
  r.source = pw_stream_new_simple(pw_main_loop_get_loop(r.loop), o.outputDescription.c_str(), sourceProps, &sourceEvents, &r);
  if (!r.capture || !r.source) throw std::runtime_error("pw_stream_new_simple failed");
  connectStream(r.source, PW_DIRECTION_OUTPUT);

  if (o.monitor) {
    pw_properties *monitorProps = pw_properties_new(
        PW_KEY_MEDIA_TYPE, "Audio", PW_KEY_MEDIA_CATEGORY, "Playback",
        PW_KEY_MEDIA_ROLE, "Communication", PW_KEY_NODE_NAME, "nv-x-self-hearing",
        PW_KEY_NODE_DESCRIPTION, "NV-X Self Hearing", nullptr);
    if (!o.monitorOutputNode.empty())
      pw_properties_set(monitorProps, PW_KEY_TARGET_OBJECT, o.monitorOutputNode.c_str());
    r.monitor = pw_stream_new_simple(pw_main_loop_get_loop(r.loop), "NV-X Self Hearing",
                                     monitorProps, &monitorEvents, &r);
    if (!r.monitor) throw std::runtime_error("self hearing stream creation failed");
    connectStream(r.monitor, PW_DIRECTION_OUTPUT);
  }
  pw_core *core = pw_stream_get_core(r.source);
  if (!core) throw std::runtime_error("pw_stream_get_core failed");
  r.registry = pw_core_get_registry(core, PW_VERSION_REGISTRY, 0);
  if (!r.registry) throw std::runtime_error("pw_core_get_registry failed");
  if (pw_registry_add_listener(r.registry, &r.registryListener, &registryEvents, &r) < 0)
    throw std::runtime_error("pw_registry_add_listener failed");

  std::thread worker([&]() {
    std::array<float, kFrameSamples> input{}, output{};
    while (r.running.load()) {
      if (r.resetRequested.exchange(false)) {
        r.input.discard();
        r.output.discard();
        r.monitorOutput.discard();
      }
      if (!r.consumerActive.load()) {
        std::this_thread::sleep_for(std::chrono::milliseconds(2));
        continue;
      }
      const bool virtualActive = r.virtualConsumerActive.load();
      const bool monitorActive = r.monitorActive.load();
      if (r.input.size() < kFrameSamples ||
          (virtualActive && r.output.size() > kRingSamples - kFrameSamples) ||
          (monitorActive && r.monitorOutput.size() > kRingSamples - kFrameSamples)) {
        std::this_thread::sleep_for(std::chrono::milliseconds(1));
        continue;
      }
      if (r.input.pop(input.data(), input.size()) != input.size()) continue;
      try { effect.run(input.data(), output.data()); }
      catch (const std::exception &e) {
        std::fprintf(stderr, "audio processing failed: %s\n", e.what());
        std::fill(output.begin(), output.end(), 0.0f);
      }
      if (!r.consumerActive.load()) continue;
      if (virtualActive && r.output.push(output.data(), output.size()) != output.size()) r.overflows++;
      if (monitorActive && r.monitorOutput.push(output.data(), output.size()) != output.size()) r.overflows++;
    }
  });
  std::fprintf(stdout, "audio_state=active\noutput_node=%s\n", o.outputNode.c_str());
  std::fflush(stdout);
  pw_main_loop_run(r.loop);
  r.running = false;
  worker.join();
  spa_hook_remove(&r.registryListener);
  pw_proxy_destroy(reinterpret_cast<pw_proxy *>(r.registry));
  if (r.monitor) pw_stream_destroy(r.monitor);
  pw_stream_destroy(r.source);
  r.source = nullptr;
  if (r.captureConnected) pw_stream_disconnect(r.capture);
  pw_stream_destroy(r.capture);
  pw_main_loop_destroy(r.loop);
  pw_deinit();
  std::fprintf(stderr, "audio counters: overflows=%llu underflows=%llu\n",
      static_cast<unsigned long long>(r.overflows.load()), static_cast<unsigned long long>(r.underflows.load()));
}
} // namespace

int main(int argc, char **argv) {
  try {
    Options o = parse(argc, argv);
    failStatus(NvAFX_InitializeLogger(LOG_LEVEL_ERROR, LOG_TARGET_STDERR, nullptr, nullptr, nullptr),
               "NvAFX_InitializeLogger");
    if (o.command == "doctor") {
      {
        Effect effect(o);
        std::array<float, kFrameSamples> input{}, output{};
        effect.run(input.data(), output.data());
      }
      std::printf("afx_ok=true\nmode=%s\nsample_rate=%u\nframe_samples=%u\n", o.mode.c_str(), kRate, kFrameSamples);
      NvAFX_UninitializeLogger();
      return 0;
    }
    if (o.command == "run") {
      runLive(o);
      NvAFX_UninitializeLogger();
      return 0;
    }
    throw std::runtime_error("unknown command " + o.command);
  } catch (const std::exception &e) {
    std::fprintf(stderr, "error: %s\n", e.what());
    std::fprintf(stderr, "usage: nv-x-audio doctor|run --mode MODE --model PATH [--input-node NAME] [--intensity 0.9]\n");
    return 1;
  }
}
