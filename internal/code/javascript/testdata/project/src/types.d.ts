declare global {
  interface Window {
    appVersion: string;
  }
  function track(event: string): void;
}

declare module 'virtual:config' {
  export const mode: string;
}

export {};
