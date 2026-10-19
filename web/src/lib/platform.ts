import { invoke as tauriInvoke } from '@tauri-apps/api/core'
import type { ApiTransport } from './api'
import { isDesktopRuntime, isWebDesktopSelector } from './desktopRuntime'
import {
  NativeSocket,
  captureNativeTrustPresentation,
  connectNative,
  disconnectNative,
  initializeNativeContext,
  nativeApiTransport,
  type NativeReply,
} from './native/nativeTransport'
import { isTrustedDesktop, trustedApp, type TrustedApp } from './native/trustedDesktop'
import { TrustedSocket } from './native/trustedSocket'

/** What the chat store needs from a live socket, whichever host opens it. */
export type PlatformSocket = WebSocket | NativeSocket | TrustedSocket

/**
 * The one seam between the app and the host it runs in. Everything that
 * differs between a plain browser and the desktop client goes through here,
 * so the rest of the app stays platform-agnostic. The window globals and IPC
 * commands behind it are the contract with the desktop client
 * (doc/desktop-trusted-client.md) and are read nowhere else.
 */
export interface Platform {
  /** The app runs inside the desktop client's native-IPC host. */
  isDesktop(): boolean
  /** The desktop client's instance selector page. */
  isWebDesktopSelector(): boolean
  /** The app runs from the desktop client's verified web bundle. */
  isTrustedDesktop(): boolean
  /** The verified bundle announcement, or null. */
  trustedApp(): TrustedApp | null
  /** Transport for API calls, or null to use same-origin fetch. */
  apiTransport(): ApiTransport | null
  /** Opens the real-time socket of this host. */
  openSocket(url: string): PlatformSocket
  /** Gaming overlay surface the host rendered this page as, or null. */
  gamingSurface(): string | null
  /** Token that authorises the gaming bridge, or null outside the client. */
  gamingBridgeToken(): string | null
  /** Calls a host command of the desktop client. */
  invoke<T = unknown>(command: string, args?: Record<string, unknown>): Promise<T>
  /** Native connection lifecycle (desktop only). */
  initializeNative(): Promise<void>
  connectNative(address: string): Promise<NativeReply>
  disconnectNative(): Promise<void>
  captureTrustPresentation(): ReturnType<typeof captureNativeTrustPresentation>
}

function windowString(name: string): string | null {
  if (typeof window === 'undefined') return null
  const value: unknown = Reflect.get(window, name)
  return typeof value === 'string' ? value : null
}

/** A browser tab, including the trusted bundle served from the client's own origin. */
export const browserPlatform: Platform = {
  isDesktop: () => false,
  isWebDesktopSelector,
  isTrustedDesktop,
  trustedApp,
  apiTransport: () => null,
  openSocket: url => isTrustedDesktop() ? new TrustedSocket() : new WebSocket(url),
  gamingSurface: () => windowString('__MNEMA_GAMING_SURFACE__'),
  gamingBridgeToken: () => windowString('__MNEMA_GAMING_BRIDGE__'),
  invoke: <T = unknown>(command: string, args?: Record<string, unknown>) => tauriInvoke<T>(command, args),
  initializeNative: initializeNativeContext,
  connectNative,
  disconnectNative,
  captureTrustPresentation: captureNativeTrustPresentation,
}

/** The desktop client's native-IPC host. */
export const desktopPlatform: Platform = {
  ...browserPlatform,
  isDesktop: () => true,
  apiTransport: () => nativeApiTransport,
  openSocket: () => new NativeSocket(),
}

function current(): Platform {
  return isDesktopRuntime() ? desktopPlatform : browserPlatform
}

/** The platform of this page. Resolved per call: the host never changes under a live page, but tests do. */
export const platform: Platform = {
  isDesktop: () => current().isDesktop(),
  isWebDesktopSelector: () => current().isWebDesktopSelector(),
  isTrustedDesktop: () => current().isTrustedDesktop(),
  trustedApp: () => current().trustedApp(),
  apiTransport: () => current().apiTransport(),
  openSocket: url => current().openSocket(url),
  gamingSurface: () => current().gamingSurface(),
  gamingBridgeToken: () => current().gamingBridgeToken(),
  invoke: <T = unknown>(command: string, args?: Record<string, unknown>) => current().invoke<T>(command, args),
  initializeNative: () => current().initializeNative(),
  connectNative: address => current().connectNative(address),
  disconnectNative: () => current().disconnectNative(),
  captureTrustPresentation: () => current().captureTrustPresentation(),
}
