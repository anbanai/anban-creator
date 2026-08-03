declare module "ali-oss" {
  interface OSSOptions {
    region?: string;
    endpoint?: string;
    bucket: string;
    accessKeyId: string;
    accessKeySecret: string;
    stsToken: string;
  }
  export default class OSS {
    constructor(options: OSSOptions);
    put(
      name: string,
      file: string | import("node:stream").Readable,
      options?: { headers?: Record<string, string>; timeout?: number },
    ): Promise<unknown>;
  }
}
