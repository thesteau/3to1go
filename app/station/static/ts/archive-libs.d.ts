// Locally generated UMD scripts. UI sources remain classic scripts without module imports.
declare const fzstd: {
  Decompress: new (
    ondata: (chunk: Uint8Array, final?: boolean) => void,
  ) => {
    push(chunk: Uint8Array, final?: boolean): void;
  };
};

declare const zip: {
  BlobReader: new (blob: Blob) => object;
  BlobWriter: new (type: string) => object;
  ZipWriter: new (
    writer: object,
    options: { level: number; useWebWorkers: boolean },
  ) => {
    add(name: string, reader: object, options: { lastModDate?: Date; signal?: AbortSignal }): Promise<unknown>;
    close(): Promise<Blob>;
  };
};
