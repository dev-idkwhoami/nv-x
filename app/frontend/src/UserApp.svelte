<script lang="ts">
  import { onMount } from 'svelte'
  import { Camera, Image, Mic, Moon, Palette, Power, RefreshCw, Settings, Sun } from '@lucide/svelte'
  import { Button } from '$lib/components/ui/button'
  import { Label } from '$lib/components/ui/label'
  import { Switch } from '$lib/components/ui/switch'
  import {
    ChooseBackgroundImage,
    GetConfig,
    GetStatus,
    RestartService,
    SaveUserSettings
  } from '../wailsjs/go/main/App'
  import type { main } from '../wailsjs/go/models'

  type Mode = 'blur' | 'replace' | 'chroma'
  type AudioMode = 'off' | 'dereverb_denoiser' | 'studio_voice_low_latency'
  type Theme = 'system' | 'light' | 'dark'
  type Page = 'video' | 'audio' | 'settings'

  let status = $state<main.AppStatus | null>(null)
  let page = $state<Page>('video')
  let saving = $state(false)
  let restarting = $state(false)
  let message = $state('Ready')
  let systemDark = $state(false)
  let hydrated = $state(false)
  let lastSavedSignature = ''
  let saveTimer: ReturnType<typeof setTimeout> | undefined
  let restartTimer: ReturnType<typeof setTimeout> | undefined

  let mode = $state<Mode>('blur')
  let cameraInput = $state('/dev/video0')
  let audioMode = $state<AudioMode>('off')
  let audioInputNode = $state('')
  let audioIntensity = $state(0.9)
  let lightEnabled = $state(false)
  let lightAddress = $state('')
  let lightBrightness = $state(20)
  let lightTemperature = $state(206)
  let blurStrength = $state(0.75)
  let chromaColor = $state('#00ff00')
  let backgroundImage = $state('')
  let theme = $state<Theme>('system')

  const modes: Array<{ value: Mode; title: string; description: string }> = [
    { value: 'blur', title: 'Background blur', description: 'Softens the room behind you' },
    { value: 'replace', title: 'Background replacement', description: 'Composites you over a selected image' },
    { value: 'chroma', title: 'Chroma key', description: 'Outputs a clean keyed color background' }
  ]
  const themes: Theme[] = ['system', 'dark', 'light']
  const chromaPalette = ['#00ff00', '#0000ff', '#ff00ff', '#ffffff', '#000000']

  let effectiveDark = $derived(theme === 'dark' || (theme === 'system' && systemDark))
  let streamState = $derived(status?.fx?.state ?? 'unknown')
  let audioState = $derived(status?.audio?.state ?? 'disabled')
  let serviceOnline = $derived(Boolean(status?.service.active))
  let cameraOnline = $derived(serviceOnline && Boolean(status?.expectedOutputExists) && (streamState === 'idle' || streamState === 'active'))
  let microphoneOnline = $derived(serviceOnline && audioState === 'active')
  let syncState = $derived(restarting ? 'Restarting service...' : saving ? 'Saving...' : message)

  $effect(() => {
    document.documentElement.classList.toggle('dark', effectiveDark)
    document.documentElement.style.colorScheme = effectiveDark ? 'dark' : 'light'
  })

  $effect(() => {
    const signature = settingsSignature()
    if (!hydrated || signature === lastSavedSignature) return
    scheduleAutoApply(signature)
  })

  onMount(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const syncSystemTheme = () => {
      systemDark = media.matches
    }
    syncSystemTheme()
    media.addEventListener('change', syncSystemTheme)
    refresh(true, true)
    const timer = window.setInterval(() => refresh(false), 3000)
    return () => {
      media.removeEventListener('change', syncSystemTheme)
      window.clearInterval(timer)
      if (saveTimer) window.clearTimeout(saveTimer)
      if (restartTimer) window.clearTimeout(restartTimer)
    }
  })

  async function refresh(reportErrors = true, hydrateForm = false) {
    try {
      const [nextStatus, nextConfig] = await Promise.all([GetStatus(), GetConfig()])
      status = nextStatus
      if (hydrateForm) {
        hydrate(nextConfig.config)
      }
    } catch (error) {
      if (reportErrors) message = formatError(error)
    }
  }

  function hydrate(cfg: any) {
    mode = normalizeMode(cfg.FX.Mode)
    const configuredCamera = cfg.Camera.InputDevice ?? '/dev/video0'
    const selectedCamera = status?.devices?.find((device) => device.Path === configuredCamera || device.StablePath === configuredCamera)
    cameraInput = selectedCamera?.StablePath || configuredCamera
    audioMode = normalizeAudioMode(cfg.Audio?.Mode)
    audioInputNode = cfg.Audio?.InputNode ?? ''
    audioIntensity = Number(cfg.Audio?.DereverbDenoiserIntensity ?? 0.9)
    lightEnabled = Boolean(cfg.Light.Enabled)
    lightAddress = cfg.Light.Address ?? ''
    lightBrightness = Number(cfg.Light.Brightness ?? 20)
    lightTemperature = Number(cfg.Light.Temperature ?? 206)
    blurStrength = Number(cfg.FX.BlurStrength ?? 0.75)
    chromaColor = cfg.FX.ChromaColor ?? '#00ff00'
    backgroundImage = cfg.FX.BackgroundImage ?? ''
    theme = normalizeTheme(cfg.UI.Theme)
    lastSavedSignature = settingsSignature()
    hydrated = true
  }

  function normalizeAudioMode(value: string): AudioMode {
    return value === 'dereverb_denoiser' || value === 'studio_voice_low_latency' ? value : 'off'
  }

  function normalizeMode(value: string): Mode {
    return value === 'replace' || value === 'chroma' ? value : 'blur'
  }

  function normalizeTheme(value: string): Theme {
    return value === 'dark' || value === 'light' ? value : 'system'
  }

  function settingsPayload(): main.UserSettings {
    return {
      cameraInput,
      mode,
      audioMode,
      audioInputNode,
      audioIntensity,
      lightEnabled,
      lightAddress,
      lightBrightness,
      lightTemperature,
      blurStrength,
      chromaColor,
      backgroundImage,
      theme
    }
  }

  function toggleAudioMode(next: Exclude<AudioMode, 'off'>) {
    audioMode = audioMode === next ? 'off' : next
  }

  function cameraDisplayName(name: string) {
    const parts = name.split(':').map((part) => part.trim())
    return parts.length === 2 && parts[0] === parts[1] ? parts[0] : name
  }

  function settingsSignature() {
    return JSON.stringify(settingsPayload())
  }

  function scheduleAutoApply(signature: string) {
    if (saveTimer) window.clearTimeout(saveTimer)
    if (restartTimer) window.clearTimeout(restartTimer)
    saveTimer = window.setTimeout(() => saveSettings(signature), 150)
    restartTimer = window.setTimeout(() => restartService(signature), 500)
  }

  async function saveSettings(signature: string) {
    saving = true
    try {
      const result = await SaveUserSettings(settingsPayload())
      if (!result.ok) {
        message = result.message
        return
      }
      lastSavedSignature = signature
      message = 'Settings saved'
    } catch (error) {
      message = formatError(error)
    } finally {
      saving = false
    }
  }

  async function restartService(signature: string) {
    if (signature !== lastSavedSignature) {
      if (saving) {
        restartTimer = window.setTimeout(() => restartService(signature), 50)
      }
      return
    }
    restarting = true
    try {
      const result = await RestartService()
      message = result.ok ? 'Settings applied; service restarted' : result.message
      await refresh(false)
    } catch (error) {
      message = formatError(error)
    } finally {
      restarting = false
    }
  }

  async function chooseBackground() {
    try {
      const path = await ChooseBackgroundImage()
      if (path) {
        backgroundImage = path
      }
    } catch (error) {
      message = formatError(error)
    }
  }

  function formatError(error: unknown) {
    return error instanceof Error ? error.message : String(error)
  }
</script>

<main class="min-h-screen bg-[#111] text-[#f3f3f3]">
  <div class="grid min-h-screen grid-cols-[92px_1fr]">
    <aside class="border-r border-[#2e2e2e] bg-[#161616]">
      <div class="h-11 border-b border-[#242424]"></div>
      <nav class="flex flex-col">
        <button
          class="flex h-[86px] flex-col items-center justify-center gap-2 border-l-4 text-sm"
          class:border-[#76b900]={page === 'audio'}
          class:border-transparent={page !== 'audio'}
          class:bg-[#242424]={page === 'audio'}
          class:text-white={page === 'audio'}
          class:text-[#aaa]={page !== 'audio'}
          onclick={() => (page = 'audio')}
        >
          <Mic size={26} />
          Audio
        </button>
        <button
          class="relative flex h-[86px] flex-col items-center justify-center gap-2 border-l-4 text-sm"
          class:border-[#76b900]={page === 'video'}
          class:border-transparent={page !== 'video'}
          class:bg-[#242424]={page === 'video'}
          class:text-white={page === 'video'}
          class:text-[#aaa]={page !== 'video'}
          onclick={() => (page = 'video')}
        >
          <Camera size={26} />
          Video
        </button>
        <button
          class="flex h-[86px] flex-col items-center justify-center gap-2 border-l-4 text-sm"
          class:border-[#76b900]={page === 'settings'}
          class:border-transparent={page !== 'settings'}
          class:bg-[#242424]={page === 'settings'}
          class:text-white={page === 'settings'}
          class:text-[#aaa]={page !== 'settings'}
          onclick={() => (page = 'settings')}
        >
          <Settings size={27} />
          Settings
        </button>
      </nav>
    </aside>

    <section class="min-w-0">
      <header class="flex h-[86px] items-center justify-between border-b border-[#202020] bg-[#262626] px-8 shadow-[0_2px_5px_rgb(0_0_0_/_45%)]">
        <div>
          <p class="text-xs uppercase tracking-[0.18em] text-[#76b900]">NV-X</p>
          <h1 class="text-[28px] font-semibold tracking-normal">{page === 'video' ? 'Video' : page === 'audio' ? 'Audio' : 'Settings'}</h1>
        </div>
        <div class="flex items-center gap-3 text-sm text-[#bdbdbd]">
          <span>{syncState}</span>
          <span class="flex items-center gap-2 border-l border-[#444] pl-3">
            <span title={`Service ${serviceOnline ? 'online' : 'offline'}`} aria-label={`Service ${serviceOnline ? 'online' : 'offline'}`}>
              <Power size={18} class={serviceOnline ? 'text-[#76b900]' : 'text-[#666]'} />
            </span>
            <span title={`Camera ${cameraOnline ? 'online' : 'offline'}`} aria-label={`Camera ${cameraOnline ? 'online' : 'offline'}`}>
              <Camera size={19} class={cameraOnline ? 'text-[#76b900]' : 'text-[#666]'} />
            </span>
            <span title={`Microphone ${microphoneOnline ? 'online' : 'offline'}`} aria-label={`Microphone ${microphoneOnline ? 'online' : 'offline'}`}>
              <Mic size={19} class={microphoneOnline ? 'text-[#76b900]' : 'text-[#666]'} />
            </span>
          </span>
          <Button variant="secondary" size="sm" onclick={() => refresh()} disabled={saving || restarting}>
            <RefreshCw size={16} />
          </Button>
        </div>
      </header>

      {#if page === 'video'}
        <section class="max-w-[760px] px-8 py-9">
          <div class="mb-8 space-y-2">
            <Label class="text-sm text-[#cfcfcf]">Camera input</Label>
            <select class="h-11 w-full border border-[#3a3a3a] bg-[#242424] px-3 text-sm text-white outline-none focus:border-[#76b900]" bind:value={cameraInput}>
              {#each status?.devices ?? [] as device}
                {#if device.Capture && device.Path !== status?.expectedOutput && device.Name !== 'NV-X Camera'}
                  <option value={device.StablePath || device.Path}>{cameraDisplayName(device.Name)}</option>
                {/if}
              {/each}
            </select>
          </div>

          <h2 class="mb-6 text-[26px] font-semibold uppercase tracking-normal text-[#76b900]">Camera effects</h2>

          <div class="divide-y divide-[#2e2e2e]">
            {#each modes as item}
              <div class="py-7">
                <button
                  class="grid w-full grid-cols-[1fr_58px] items-center gap-6 text-left"
                  onclick={() => (mode = item.value)}
                >
                  <span>
                    <span class="block text-[24px] font-semibold text-white">{item.title}</span>
                    <span class="mt-1 block text-[18px] leading-snug text-[#aaa]">{item.description}</span>
                  </span>
                  <span class="relative h-6 w-12 rounded-full transition" class:bg-[#76b900]={mode === item.value} class:bg-[#555]={mode !== item.value}>
                    <span class="absolute top-0.5 h-5 w-5 rounded-full bg-white transition" class:left-[26px]={mode === item.value} class:left-0.5={mode !== item.value}></span>
                  </span>
                </button>

                {#if mode === item.value}
                  <div class="mt-5 border-l-2 border-[#76b900] pl-5">
                    {#if mode === 'blur'}
                      <div class="space-y-3">
                        <div class="flex justify-between text-sm">
                          <Label class="text-[#cfcfcf]">Blur strength</Label>
                          <span>{Math.round(blurStrength * 100)}%</span>
                        </div>
                        <input class="w-full accent-[#76b900]" type="range" min="0" max="1" step="0.01" bind:value={blurStrength} />
                      </div>
                    {:else if mode === 'replace'}
                      <div class="space-y-3">
                        <Label class="text-[#cfcfcf]">Background image</Label>
                        <div class="flex gap-2">
                          <input class="h-10 min-w-0 flex-1 border border-[#3a3a3a] bg-[#242424] px-3 text-sm text-white outline-none" bind:value={backgroundImage} />
                          <Button variant="secondary" onclick={chooseBackground}>
                            <Image size={17} />
                            Browse
                          </Button>
                        </div>
                      </div>
                    {:else if mode === 'chroma'}
                      <div class="space-y-3">
                        <Label class="text-[#cfcfcf]">Chroma color</Label>
                        <div class="flex items-center gap-3">
                          <input class="h-10 w-14 border border-[#444] bg-transparent" type="color" bind:value={chromaColor} />
                          <div class="flex gap-2">
                            {#each chromaPalette as color}
                              <button
                                class="h-8 w-8 border border-[#555]"
                                style={`background:${color}`}
                                aria-label={`Use ${color}`}
                                onclick={() => (chromaColor = color)}
                              ></button>
                            {/each}
                          </div>
                        </div>
                      </div>
                    {/if}
                  </div>
                {/if}
              </div>
            {/each}
          </div>
        </section>
      {:else if page === 'audio'}
        <section class="max-w-[760px] px-8 py-9">
          <div class="mb-8 space-y-2">
            <Label class="text-sm text-[#cfcfcf]">Microphone input</Label>
            <select class="h-11 w-full border border-[#3a3a3a] bg-[#242424] px-3 text-sm text-white outline-none focus:border-[#76b900]" bind:value={audioInputNode}>
              <option value="">System Default</option>
              {#each status?.audioSources ?? [] as source}
                <option value={source.nodeName}>{source.description}{source.default ? ' · Default' : ''}</option>
              {/each}
            </select>
          </div>

          <h2 class="mb-6 text-[26px] font-semibold uppercase tracking-normal text-[#76b900]">Microphone effects</h2>
          <div class="divide-y divide-[#2e2e2e]">
            <div class="py-7">
              <button class="grid w-full grid-cols-[1fr_58px] items-center gap-6 text-left" onclick={() => toggleAudioMode('dereverb_denoiser')}>
                <span>
                  <span class="block text-[24px] font-semibold text-white">Noise & room echo removal</span>
                  <span class="mt-1 block text-[18px] leading-snug text-[#aaa]">Reduces background noise and room reverberation.</span>
                </span>
                <span class="relative h-6 w-12 rounded-full transition" class:bg-[#76b900]={audioMode === 'dereverb_denoiser'} class:bg-[#555]={audioMode !== 'dereverb_denoiser'}>
                  <span class="absolute top-0.5 h-5 w-5 rounded-full bg-white transition" class:left-[26px]={audioMode === 'dereverb_denoiser'} class:left-0.5={audioMode !== 'dereverb_denoiser'}></span>
                </span>
              </button>
              {#if audioMode === 'dereverb_denoiser'}
                <div class="mt-5 space-y-3 border-l-2 border-[#76b900] pl-5">
                  <div class="flex justify-between text-sm"><Label class="text-[#cfcfcf]">Intensity</Label><span>{Math.round(audioIntensity * 100)}%</span></div>
                  <input class="w-full accent-[#76b900]" type="range" min="0" max="1" step="0.01" bind:value={audioIntensity} />
                </div>
              {/if}
            </div>

            <div class="py-7">
              <button class="grid w-full grid-cols-[1fr_58px] items-center gap-6 text-left" onclick={() => toggleAudioMode('studio_voice_low_latency')}>
                <span>
                  <span class="block text-[24px] font-semibold text-white">Studio Voice</span>
                  <span class="mt-1 block text-[18px] leading-snug text-[#aaa]">Reconstructs clear studio-style speech. May add up to about 110 ms latency.</span>
                </span>
                <span class="relative h-6 w-12 rounded-full transition" class:bg-[#76b900]={audioMode === 'studio_voice_low_latency'} class:bg-[#555]={audioMode !== 'studio_voice_low_latency'}>
                  <span class="absolute top-0.5 h-5 w-5 rounded-full bg-white transition" class:left-[26px]={audioMode === 'studio_voice_low_latency'} class:left-0.5={audioMode !== 'studio_voice_low_latency'}></span>
                </span>
              </button>
            </div>
          </div>
          {#if audioMode === 'off'}
            <p class="mt-6 text-sm text-[#aaa]">Both effects are off. NV-X Microphone will not be published.</p>
          {/if}
        </section>
      {:else}
        <section class="max-w-[720px] px-8 py-9">
          <div class="mb-8">
            <h2 class="text-[26px] font-semibold uppercase tracking-normal text-[#76b900]">Application settings</h2>
            <p class="mt-2 text-sm text-[#aaa]">Changes are saved automatically and restart the user service after a short delay.</p>
          </div>

          <div class="space-y-8 divide-y divide-[#2e2e2e]">
            <div class="space-y-5 pb-8">
              <div class="flex items-center justify-between gap-4">
                <div>
                  <p class="text-[22px] font-semibold">Ring light auto-control</p>
                  <p class="text-sm text-[#aaa]">Turns on while the camera is being viewed.</p>
                </div>
                <Switch bind:checked={lightEnabled} disabled={saving || restarting} />
              </div>

              <div class="space-y-2">
                <Label class="text-sm text-[#cfcfcf]">Address</Label>
                <input
                  class="h-10 w-full border border-[#3a3a3a] bg-[#242424] px-3 text-sm text-white outline-none focus:border-[#76b900]"
                  placeholder="Auto from elgato-light-toggle"
                  bind:value={lightAddress}
                />
              </div>

              <div class="space-y-3">
                <div class="flex justify-between text-sm">
                  <Label class="text-[#cfcfcf]">Brightness</Label>
                  <span>{lightBrightness}%</span>
                </div>
                <input class="w-full accent-[#76b900]" type="range" min="0" max="100" bind:value={lightBrightness} />
              </div>

              <div class="space-y-3">
                <div class="flex justify-between text-sm">
                  <Label class="text-[#cfcfcf]">Temperature</Label>
                  <span>{lightTemperature}</span>
                </div>
                <input class="w-full accent-[#76b900]" type="range" min="143" max="344" bind:value={lightTemperature} />
              </div>
            </div>

            <div class="space-y-3 pt-8">
              <Label class="text-[22px] font-semibold">Theme</Label>
              <div class="grid grid-cols-3 gap-2">
                {#each themes as option}
                  <button
                    class="flex h-11 items-center justify-center gap-2 border border-[#3a3a3a] bg-[#242424] text-sm capitalize text-white"
                    class:border-[#76b900]={theme === option}
                    onclick={() => (theme = option)}
                  >
                    {#if option === 'light'}<Sun size={16} />{:else if option === 'dark'}<Moon size={16} />{:else}<Palette size={16} />{/if}
                    {option}
                  </button>
                {/each}
              </div>
            </div>
          </div>
        </section>
      {/if}
    </section>
  </div>
</main>
