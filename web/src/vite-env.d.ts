/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Commit the image was built from (Docker build arg APP_VERSION). */
  readonly VITE_APP_VERSION?: string;
  /** Where feedback links point (Docker build arg ISSUES_URL). */
  readonly VITE_ISSUES_URL?: string;
}
