interface ImportMetaEnv {
  readonly VITE_BASE_URL: string;
  readonly VITE_HABITAT_DOMAIN: string;
  readonly VITE_HASH_ROUTING?: string;
  readonly VITE_DOCS_SERVER_DID?: string;
  readonly VITE_HOME_SERVER_DID?: string;
}

declare module "*.svg" {
  const content: string;
  export default content;
}
