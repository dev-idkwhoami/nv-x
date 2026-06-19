<script lang="ts">
  import { onMount } from 'svelte'
  import { Camera, Image, Moon, Palette, RefreshCw, Settings, Sun } from '@lucide/svelte'
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
  type Theme = 'system' | 'light' | 'dark'
  type Page = 'video' | 'settings'

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
  let serviceState = $derived(status?.service.active ? 'Running' : 'Stopped')
  let streamState = $derived(status?.fx?.state ?? 'unknown')
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

  function normalizeMode(value: string): Mode {
    return value === 'replace' || value === 'chroma' ? value : 'blur'
  }

  function normalizeTheme(value: string): Theme {
    return value === 'dark' || value === 'light' ? value : 'system'
  }

  function settingsPayload(): main.UserSettings {
    return {
      mode,
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
          <p class="text-xs uppercase tracking-[0.18em] text-[#76b900]">NV-vCam</p>
          <h1 class="text-[28px] font-semibold tracking-normal">{page === 'video' ? 'Video' : 'Settings'}</h1>
        </div>
        <div class="flex items-center gap-3 text-sm text-[#bdbdbd]">
          <span>{syncState}</span>
          <span>{serviceState}</span>
          <span class="h-1.5 w-1.5 rounded-full" class:bg-[#76b900]={status?.service.active} class:bg-[#6b6b6b]={!status?.service.active}></span>
          <Button variant="secondary" size="sm" onclick={() => refresh()} disabled={saving || restarting}>
            <RefreshCw size={16} />
          </Button>
        </div>
      </header>

      {#if page === 'video'}
        <section class="max-w-[760px] px-8 py-9">
          <div class="mb-8 flex h-[42px] items-center bg-[#353535] px-4 text-sm text-[#d6d6d6]">
            <Camera class="mr-3 text-[#d6d6d6]" size={20} />
            <span class="truncate">{status?.expectedOutput ?? '/dev/video10'} · {streamState}</span>
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
