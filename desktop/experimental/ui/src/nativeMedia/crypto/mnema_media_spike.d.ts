/* tslint:disable */
/* eslint-disable */

export class MediaCryptor {
    free(): void;
    [Symbol.dispose](): void;
    decrypt(input: Uint8Array, codec: string): Uint8Array;
    encrypt(input: Uint8Array, codec: string): Uint8Array;
    constructor(base_key: Uint8Array, context: number, epoch: number, sender: number);
}

export function decrypt_known_vector(base_key: Uint8Array, key_id: number, metadata: Uint8Array, ciphertext: Uint8Array): Uint8Array;

export function decrypt_known_vector_low_level(base_key: Uint8Array, key_id: number, metadata: Uint8Array, ciphertext: Uint8Array): Uint8Array;

export function encrypt_known_vector_low_level(base_key: Uint8Array, key_id: number, counter: number, metadata: Uint8Array, plaintext: Uint8Array): Uint8Array;

export type InitInput = RequestInfo | URL | Response | BufferSource | WebAssembly.Module;

export interface InitOutput {
    readonly memory: WebAssembly.Memory;
    readonly __wbg_mediacryptor_free: (a: number, b: number) => void;
    readonly decrypt_known_vector: (a: number, b: number, c: number, d: number, e: number, f: number, g: number) => [number, number, number, number];
    readonly decrypt_known_vector_low_level: (a: number, b: number, c: number, d: number, e: number, f: number, g: number) => [number, number, number, number];
    readonly encrypt_known_vector_low_level: (a: number, b: number, c: number, d: number, e: number, f: number, g: number, h: number) => [number, number, number, number];
    readonly mediacryptor_decrypt: (a: number, b: number, c: number, d: number, e: number) => [number, number, number, number];
    readonly mediacryptor_encrypt: (a: number, b: number, c: number, d: number, e: number) => [number, number, number, number];
    readonly mediacryptor_new: (a: number, b: number, c: number, d: number, e: number) => [number, number, number];
    readonly __wbindgen_externrefs: WebAssembly.Table;
    readonly __wbindgen_malloc: (a: number, b: number) => number;
    readonly __externref_table_dealloc: (a: number) => void;
    readonly __wbindgen_free: (a: number, b: number, c: number) => void;
    readonly __wbindgen_realloc: (a: number, b: number, c: number, d: number) => number;
    readonly __wbindgen_start: () => void;
}

export type SyncInitInput = BufferSource | WebAssembly.Module;

/**
 * Instantiates the given `module`, which can either be bytes or
 * a precompiled `WebAssembly.Module`.
 *
 * @param {{ module: SyncInitInput }} module - Passing `SyncInitInput` directly is deprecated.
 *
 * @returns {InitOutput}
 */
export function initSync(module: { module: SyncInitInput } | SyncInitInput): InitOutput;

/**
 * If `module_or_path` is {RequestInfo} or {URL}, makes a request and
 * for everything else, calls `WebAssembly.instantiate` directly.
 *
 * @param {{ module_or_path: InitInput | Promise<InitInput> }} module_or_path - Passing `InitInput` directly is deprecated.
 *
 * @returns {Promise<InitOutput>}
 */
export default function __wbg_init (module_or_path?: { module_or_path: InitInput | Promise<InitInput> } | InitInput | Promise<InitInput>): Promise<InitOutput>;
