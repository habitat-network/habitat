import {
  exchangeCodeAsync,
  loadAsync,
  makeRedirectUri,
} from "expo-auth-session";
import {
  createContext,
  PropsWithChildren,
  useContext,
  useMemo,
  useState,
} from "react";
import * as SecureStore from "expo-secure-store";
import { useQuery, useQueryClient } from "@tanstack/react-query";

const clientId = "https://sashankg.github.io/client-metadata.json"; // fake for now
export const domain = "privi.taile529e.ts.net";
const redirectUri = makeRedirectUri({
  scheme: "habitat.camera",
  path: "oauth",
});

const issuer = {
  authorizationEndpoint: `https://${domain}/oauth/authorize`,
  tokenEndpoint: `https://${domain}/oauth/token`,
};

const secureStoreKeyToken = "token";
const secureStoreKeyDID = "did";

export type FetchWithAuth = (
  url: string,
  options?: Parameters<typeof fetch>[1],
) => Promise<Response>;

interface AuthContextData {
  signIn: (handle: string) => Promise<void>;
  signOut: () => void;
  token: string | null;
  isLoading: boolean;
  fetchWithAuth: FetchWithAuth;
  did: string | null;
}

const AuthContext = createContext<AuthContextData>({
  signIn: async () => {},
  signOut: () => {},
  token: null,
  isLoading: false,
  fetchWithAuth: fetch,
  did: null,
});

export const useAuth = () => useContext(AuthContext);

export const AuthProvider = ({ children }: PropsWithChildren) => {
  const resolveHandle = async (handle: string) => {
    const resp = await fetch(
      `https://bsky.social/xrpc/com.atproto.identity.resolveHandle?handle=${handle}`,
    );
    const did = (await resp.json())["did"];
    return did;
  };
  const queryClient = useQueryClient();
  const { data: info, isLoading } = useQuery({
    queryKey: ["token"],
    queryFn: async () => {
      const token = await SecureStore.getItemAsync(secureStoreKeyToken);
      const did = await SecureStore.getItemAsync(secureStoreKeyDID);
      if (!token) return null;
      return {
        token,
        did,
      };
    },
  });
  const value = useMemo<AuthContextData>(
    () => ({
      signIn: async (newHandle: string) => {
        const authRequest = await loadAsync(
          {
            extraParams: {
              handle: newHandle,
            },
            clientId: clientId,
            scopes: [],
            redirectUri: redirectUri,
          },
          issuer,
        );
        const authResponse = await authRequest.promptAsync(issuer);
        if (authResponse.type !== "success") return;
        const tokenResponse = await exchangeCodeAsync(
          {
            clientId,
            code: authResponse.params.code,
            redirectUri,
            extraParams: {
              code_verifier: authRequest.codeVerifier ?? "",
            },
          },
          issuer,
        );
        await SecureStore.setItemAsync(
          secureStoreKeyToken,
          tokenResponse.accessToken,
        );
        const did = await resolveHandle(newHandle);
        await SecureStore.setItemAsync(secureStoreKeyDID, did);
        await queryClient.invalidateQueries({ queryKey: ["token"] });
      },
      token: info?.token ?? null,
      did: info?.did ?? null,
      signOut: () => {
        SecureStore.deleteItemAsync(secureStoreKeyToken);
        SecureStore.deleteItemAsync(secureStoreKeyDID);
        queryClient.invalidateQueries({ queryKey: ["token"] });
      },
      isLoading,
      fetchWithAuth: (url, options) => {
        return fetch(new URL(url, `https://${domain}`), {
          ...options,
          headers: {
            Authorization: `Bearer ${info?.token}`,
            "Habitat-Auth-Method": "oauth",
            ...options?.headers,
          },
        });
      },
    }),
    [info, isLoading, queryClient],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
};
