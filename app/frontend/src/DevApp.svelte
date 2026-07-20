<script lang="ts">
  import { onMount } from 'svelte'
  import { Alert, AlertDescription, AlertTitle } from '$lib/components/ui/alert'
  import { Badge } from '$lib/components/ui/badge'
  import { Button } from '$lib/components/ui/button'
  import {
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle
  } from '$lib/components/ui/card'
  import { Label } from '$lib/components/ui/label'
  import { ScrollArea } from '$lib/components/ui/scroll-area'
  import { Separator } from '$lib/components/ui/separator'
  import { Switch } from '$lib/components/ui/switch'
  import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow
  } from '$lib/components/ui/table'
  import { Tabs, TabsContent, TabsList, TabsTrigger } from '$lib/components/ui/tabs'
  import { Textarea } from '$lib/components/ui/textarea'
  import {
    GetConfig,
    GetServiceStatus,
    GetStatus,
    InstallService,
    ReloadLoopback,
    RestartService,
    SetTheme,
    ShowLoopback,
    StartService,
    StopService,
    WriteConfig,
    WriteLoopback
  } from '../wailsjs/go/main/App'
  import type { main } from '../wailsjs/go/models'

  let status = $state<main.AppStatus | null>(null)
  let configView = $state<main.ConfigView | null>(null)
  let loopbackView = $state<main.LoopbackView | null>(null)
  let busy = $state(false)
  let log = $state('Ready.')

  let configForce = $state(false)
  let configDryRun = $state(true)
  let loopbackForce = $state(false)
  let loopbackDryRun = $state(true)
  let reloadDryRun = $state(true)
  let installForce = $state(false)
  let installDryRun = $state(true)
  let installEnable = $state(false)
  let installStart = $state(false)
  let theme = $state<'system' | 'light' | 'dark'>('system')
  let systemDark = $state(false)

  const statusTone = (ok: boolean | undefined) => (ok ? 'default' : 'secondary')
  const boolText = (value: boolean | undefined) => (value ? 'Yes' : 'No')

  let effectiveDark = $derived(theme === 'dark' || (theme === 'system' && systemDark))
  const themes = ['system', 'light', 'dark'] as const

  $effect(() => {
    document.documentElement.classList.toggle('dark', effectiveDark)
    document.documentElement.style.colorScheme = effectiveDark ? 'dark' : 'light'
  })

  onMount(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const syncSystemTheme = () => {
      systemDark = media.matches
    }
    syncSystemTheme()
    media.addEventListener('change', syncSystemTheme)
    refresh()
    return () => {
      media.removeEventListener('change', syncSystemTheme)
    }
  })

  async function refresh(reportErrors = true) {
    busy = true
    try {
      const [nextStatus, nextConfig, nextLoopback] = await Promise.all([
        GetStatus(),
        GetConfig(),
        ShowLoopback()
      ])
      status = nextStatus
      configView = nextConfig
      loopbackView = nextLoopback
      theme = normalizeTheme(nextConfig.config.UI.Theme)
    } catch (error) {
      if (reportErrors) {
        log = formatError(error)
      } else {
        throw error
      }
    } finally {
      busy = false
    }
  }

  async function runAction(label: string, action: () => Promise<main.ActionResult>) {
    busy = true
    try {
      const result = await action()
      log = [result.ok ? label : `Failed: ${label}`, result.message, result.output]
        .filter(Boolean)
        .join('\n\n')
      try {
        await refresh(false)
      } catch (refreshError) {
        log = [log, `Refresh failed: ${formatError(refreshError)}`].filter(Boolean).join('\n\n')
      }
    } catch (error) {
      log = formatError(error)
    } finally {
      busy = false
    }
  }

  async function readServiceStatus() {
    busy = true
    try {
      const result = await GetServiceStatus()
      log = [result.message, result.output].filter(Boolean).join('\n\n')
    } catch (error) {
      log = formatError(error)
    } finally {
      busy = false
    }
  }

  function formatError(error: unknown) {
    return error instanceof Error ? error.message : String(error)
  }

  function normalizeTheme(value: string): 'system' | 'light' | 'dark' {
    return value === 'light' || value === 'dark' ? value : 'system'
  }

  async function changeTheme(nextTheme: 'system' | 'light' | 'dark') {
    theme = nextTheme
    await runAction(`Theme ${nextTheme}`, () => SetTheme(nextTheme))
  }
</script>

<main class="min-h-screen bg-background text-foreground">
  <div class="mx-auto flex min-h-screen w-full max-w-6xl flex-col gap-5 px-6 py-6">
    <header class="flex flex-col gap-4 border-b pb-5 md:flex-row md:items-end md:justify-between">
      <div class="space-y-1">
        <p class="text-sm font-medium text-muted-foreground">nv-x</p>
        <h1 class="text-3xl font-semibold tracking-normal">Virtual camera manager</h1>
        <p class="max-w-2xl text-sm text-muted-foreground">
          Manage device discovery, config generation, loopback setup, and the user service.
        </p>
      </div>
      <Button variant="secondary" onclick={() => refresh()} disabled={busy}>
        {busy ? 'Working...' : 'Refresh'}
      </Button>
    </header>

    {#if status?.service.error}
      <Alert variant="destructive">
        <AlertTitle>Systemd user service status unavailable</AlertTitle>
        <AlertDescription>{status.service.error}</AlertDescription>
      </Alert>
    {/if}

    {#if loopbackView?.conflict}
      <Alert>
        <AlertTitle>Loopback config conflict detected</AlertTitle>
        <AlertDescription>{loopbackView.warning}</AlertDescription>
      </Alert>
    {/if}

    <section class="grid gap-4 md:grid-cols-4">
      {#each [
        ['v4l2loopback', boolText(status?.v4l2LoopbackLoaded), status?.v4l2LoopbackLoaded],
        ['Cam Link input', status?.expectedInput ?? '/dev/video0', status?.expectedInputExists],
        ['NV-X output', status?.fx?.state ?? 'unknown', status?.fx?.state === 'idle' || status?.fx?.state === 'active'],
        ['Service', status?.service.name ?? 'nv-x.service', status?.service.active]
      ] as [title, value, ok]}
        <Card>
          <CardHeader class="gap-2">
            <div class="flex items-center justify-between gap-2">
              <CardDescription>{title}</CardDescription>
              <Badge variant={statusTone(Boolean(ok))}>{ok ? 'OK' : 'Check'}</Badge>
            </div>
            <CardTitle class="truncate text-base">{value}</CardTitle>
          </CardHeader>
        </Card>
      {/each}
    </section>

    <Tabs value="status" class="flex-1">
      <TabsList>
        <TabsTrigger value="status">Status</TabsTrigger>
        <TabsTrigger value="config">Config</TabsTrigger>
        <TabsTrigger value="loopback">Loopback</TabsTrigger>
        <TabsTrigger value="service">Service</TabsTrigger>
        <TabsTrigger value="log">Log</TabsTrigger>
      </TabsList>

      <TabsContent value="status" class="mt-4">
        <Card>
          <CardHeader>
            <CardTitle>Detected video devices</CardTitle>
            <CardDescription>Devices are read from sysfs and matched to expected paths.</CardDescription>
          </CardHeader>
          <CardContent>
            {#if status?.devices?.length}
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Path</TableHead>
                    <TableHead>Sysfs</TableHead>
                    <TableHead>Name</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {#each status.devices as device}
                    <TableRow>
                      <TableCell class="font-mono">{device.Path}</TableCell>
                      <TableCell>{device.SysName}</TableCell>
                      <TableCell>{device.Name}</TableCell>
                    </TableRow>
                  {/each}
                </TableBody>
              </Table>
            {:else}
              <p class="text-sm text-muted-foreground">No video devices detected.</p>
            {/if}
          </CardContent>
        </Card>
      </TabsContent>

      <TabsContent value="config" class="mt-4">
        <div class="grid gap-4 lg:grid-cols-[1fr_280px]">
          <Card>
            <CardHeader>
              <CardTitle>Effective config</CardTitle>
              <CardDescription>{configView?.found ? configView.path : `Defaults, not yet written to ${configView?.path ?? ''}`}</CardDescription>
            </CardHeader>
            <CardContent>
              <Textarea class="min-h-80 font-mono text-xs" readonly value={configView?.rendered ?? ''} />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Write config</CardTitle>
              <CardDescription>Create or overwrite the user config file.</CardDescription>
            </CardHeader>
            <CardContent class="space-y-4">
              <div class="space-y-2">
                <Label class="text-sm">Theme</Label>
                <div class="grid grid-cols-3 gap-2">
                  {#each themes as option}
                    <Button
                      type="button"
                      variant={theme === option ? 'default' : 'secondary'}
                      onclick={() => changeTheme(option)}
                      disabled={busy}
                    >
                      {option}
                    </Button>
                  {/each}
                </div>
              </div>
              <Separator />
              <div class="flex items-center justify-between gap-3 rounded-lg border p-3">
                <Label class="text-sm">Dry run</Label>
                <Switch bind:checked={configDryRun} />
              </div>
              <div class="flex items-center justify-between gap-3 rounded-lg border p-3">
                <Label class="text-sm">Force overwrite</Label>
                <Switch bind:checked={configForce} />
              </div>
              <Button class="w-full" onclick={() => runAction('Config write', () => WriteConfig(configForce, configDryRun))} disabled={busy}>
                Write config
              </Button>
            </CardContent>
          </Card>
        </div>
      </TabsContent>

      <TabsContent value="loopback" class="mt-4">
        <div class="grid gap-4 lg:grid-cols-[1fr_320px]">
          <Card>
            <CardHeader>
              <CardTitle>v4l2loopback files</CardTitle>
              <CardDescription>Target: {loopbackView?.targetPath}</CardDescription>
            </CardHeader>
            <CardContent class="space-y-4">
              {#if loopbackView?.found?.length}
                {#each loopbackView.found as file}
                  <div class="rounded-lg border bg-muted/20 p-3">
                    <div class="mb-2 flex items-center justify-between gap-3">
                      <p class="truncate text-sm font-medium">{file.Path}</p>
                      <Badge variant={file.IsNV ? 'default' : 'secondary'}>{file.IsNV ? 'nv-x' : 'external'}</Badge>
                    </div>
                    <pre class="overflow-x-auto whitespace-pre-wrap text-xs text-muted-foreground">{file.Content}</pre>
                  </div>
                {/each}
              {:else}
                <p class="text-sm text-muted-foreground">No v4l2loopback config files found.</p>
              {/if}
              <Separator />
              <div>
                <p class="mb-2 text-sm font-medium">Generated nv-x config</p>
                <pre class="overflow-x-auto rounded-lg border bg-muted/20 p-3 text-xs">{loopbackView?.rendered}</pre>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Loopback actions</CardTitle>
              <CardDescription>Write config and reload the kernel module.</CardDescription>
            </CardHeader>
            <CardContent class="space-y-4">
              <div class="flex items-center justify-between gap-3 rounded-lg border p-3">
                <Label class="text-sm">Write dry run</Label>
                <Switch bind:checked={loopbackDryRun} />
              </div>
              <div class="flex items-center justify-between gap-3 rounded-lg border p-3">
                <Label class="text-sm">Force conflict</Label>
                <Switch bind:checked={loopbackForce} />
              </div>
              <Button class="w-full" onclick={() => runAction('Loopback write', () => WriteLoopback(loopbackForce, loopbackDryRun))} disabled={busy}>
                Write loopback config
              </Button>
              <Separator />
              <div class="flex items-center justify-between gap-3 rounded-lg border p-3">
                <Label class="text-sm">Reload dry run</Label>
                <Switch bind:checked={reloadDryRun} />
              </div>
              <Button variant="secondary" class="w-full" onclick={() => runAction('Loopback reload', () => ReloadLoopback(reloadDryRun))} disabled={busy}>
                Reload loopback
              </Button>
            </CardContent>
          </Card>
        </div>
      </TabsContent>

      <TabsContent value="service" class="mt-4">
        <div class="grid gap-4 lg:grid-cols-[1fr_320px]">
          <Card>
            <CardHeader>
              <CardTitle>Service state</CardTitle>
              <CardDescription>{status?.service.name}</CardDescription>
            </CardHeader>
            <CardContent class="space-y-4">
              <div class="flex flex-wrap gap-2">
                <Badge variant={statusTone(status?.service.exists)}>File: {boolText(status?.service.exists)}</Badge>
                <Badge variant={statusTone(status?.service.active)}>Active: {boolText(status?.service.active)}</Badge>
                <Badge variant={statusTone(status?.loopbackConfigExists)}>Loopback config: {boolText(status?.loopbackConfigExists)}</Badge>
                <Badge variant={statusTone(status?.expectedInputExists)}>Input: {status?.expectedInput ?? '/dev/video0'}</Badge>
                <Badge variant={statusTone(status?.fx?.state === 'idle' || status?.fx?.state === 'active')}>Native FX: {status?.fx?.state ?? 'unknown'}</Badge>
              </div>
              <div class="grid gap-3">
                <div class="rounded-lg border bg-muted/20 p-3 text-sm">
                  <p class="mb-3 font-medium">Native Maxine stream</p>
                  <div class="grid gap-2 sm:grid-cols-2">
                    <div>
                      <p class="text-muted-foreground">Input</p>
                      <p class="font-mono">{status?.expectedInput ?? '/dev/video0'}</p>
                    </div>
                    <div>
                      <p class="text-muted-foreground">Device</p>
                      <p class="font-mono">{status?.fx?.device ?? '/dev/video10'}</p>
                    </div>
                    <div>
                      <p class="text-muted-foreground">External consumers</p>
                      <p>{status?.fx?.consumers ?? 0}</p>
                    </div>
                    <div>
                      <p class="text-muted-foreground">Dependencies</p>
                      <p>{status?.fx?.dependencies?.length ? `Missing ${status.fx.dependencies.join(', ')}` : 'OK'}</p>
                    </div>
                    <div>
                      <p class="text-muted-foreground">Updated</p>
                      <p>{status?.fx?.updatedAt ?? 'not available'}</p>
                    </div>
                  </div>
                  {#if status?.fx?.message}
                    <Separator class="my-3" />
                    <p class="text-muted-foreground">{status.fx.message}</p>
                  {/if}
                </div>
              </div>
              <Button variant="secondary" onclick={readServiceStatus} disabled={busy}>Read systemctl status</Button>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Service actions</CardTitle>
              <CardDescription>Install and control the user unit.</CardDescription>
            </CardHeader>
            <CardContent class="space-y-4">
              <div class="flex items-center justify-between gap-3 rounded-lg border p-3">
                <Label class="text-sm">Install dry run</Label>
                <Switch bind:checked={installDryRun} />
              </div>
              <div class="flex items-center justify-between gap-3 rounded-lg border p-3">
                <Label class="text-sm">Force overwrite</Label>
                <Switch bind:checked={installForce} />
              </div>
              <div class="flex items-center justify-between gap-3 rounded-lg border p-3">
                <Label class="text-sm">Enable after install</Label>
                <Switch bind:checked={installEnable} />
              </div>
              <div class="flex items-center justify-between gap-3 rounded-lg border p-3">
                <Label class="text-sm">Start after install</Label>
                <Switch bind:checked={installStart} />
              </div>
              <Button class="w-full" onclick={() => runAction('Service install', () => InstallService(installForce, installEnable, installStart, installDryRun))} disabled={busy}>
                Install service
              </Button>
              <div class="grid grid-cols-3 gap-2">
                <Button variant="secondary" onclick={() => runAction('Service start', StartService)} disabled={busy}>Start</Button>
                <Button variant="secondary" onclick={() => runAction('Service stop', StopService)} disabled={busy}>Stop</Button>
                <Button variant="secondary" onclick={() => runAction('Service restart', RestartService)} disabled={busy}>Restart</Button>
              </div>
            </CardContent>
          </Card>
        </div>
      </TabsContent>

      <TabsContent value="log" class="mt-4">
        <Card>
          <CardHeader>
            <CardTitle>Command output</CardTitle>
            <CardDescription>Backend messages and command output from the latest action.</CardDescription>
          </CardHeader>
          <CardContent>
            <ScrollArea class="h-96 rounded-lg border bg-muted/20 p-4">
              <pre class="whitespace-pre-wrap text-xs">{log}</pre>
            </ScrollArea>
          </CardContent>
        </Card>
      </TabsContent>
    </Tabs>
  </div>
</main>
