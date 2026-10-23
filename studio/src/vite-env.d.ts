/// <reference types="vite/client" />

declare const __APP_COMMIT_SHA__: string

declare module '*.css' {
  const content: string
  export default content
}
