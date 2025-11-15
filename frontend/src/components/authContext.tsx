"use client";

import React, {
  createContext,
  useContext,
  useState,
  type ReactNode,
  useEffect,
} from "react";
import Cookies from "js-cookie";
import { useRouter } from "@tanstack/react-router";

interface AuthContextType {
  isAuthenticated: boolean;
  handle: string | null;
  login: (
    email: string,
    password: string,
    redirectRoute: string | null,
    source: string | null,
  ) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider: React.FC<{ children: ReactNode }> = ({
  children,
}) => {
  const isAuthenticatedHelper = (): boolean => {
    const token = Cookies.get("access_token");
    const authed = token ? true : false;
    console.log("authed", authed);
    return authed;
  };

  const [isAuthenticated, setIsAuthenticated] = useState<boolean>(
    isAuthenticatedHelper(),
  );

  const [handle, setHandle] = useState<string | null>(null);

  useEffect(() => {
    const did = Cookies.get("did");
    const authed = did ? true : false;
    setIsAuthenticated(authed);
  }, []);

  useEffect(() => {
    const handle = Cookies.get("handle");
    if (handle) {
      setHandle(handle);
    }
  }, []);

  const router = useRouter();

  const login = async (
    identifier: string,
    password: string,
    redirectRoute: string | null = null,
    source: string | null = null,
  ) => {
    try {
      // If we are using a *ts.net domain, make sure the cookies are applied to all other subdomains on that TailNet.
      let parentDomain = window.location.hostname;
      if (window.location.hostname.endsWith(".ts.net")) {
        const parts = window.location.hostname.split(".");
        if (parts.length > 2) {
          parentDomain = parts.slice(-3).join(".");
        }
      }

      const fullHandle =
        parentDomain == "localhost"
          ? `${identifier}`
          : `${identifier}.${window.location.hostname}`;
      const response = await fetch(
        `${window.location.origin}/xrpc/com.atproto.server.createSession`,
        {
          method: "POST",
          body: JSON.stringify({
            password: password,
            identifier: fullHandle,
          }),
          headers: {
            "Content-Type": "application/json",
          },
        },
      );

      // TODO: handle 401 Unauthorized
      const respBody = await response.json();

      if (response.status != 200) {
        throw new Error(respBody);
      }
      const { accessJwt, refreshJwt, did, handle } = respBody;

      // Set the access token in a cookie
      Cookies.set("access_token", accessJwt, {
        expires: 7,
        ...(parentDomain != "localhost" && { domain: parentDomain }),
      });
      Cookies.set("refresh_token", refreshJwt, {
        expires: 7,
        ...(parentDomain != "localhost" && { domain: parentDomain }),
      });
      // The user's did
      Cookies.set("user_did", did, {
        expires: 7,
        ...(parentDomain != "localhost" && { domain: parentDomain }),
      });

      Cookies.set("handle", handle, {
        expires: 7,
        ...(parentDomain != "localhost" && { domain: parentDomain }),
      });
      // To help dev app frontends figure out where to make API requests.
      Cookies.set("habitat_domain", window.location.hostname, {
        expires: 7,
        domain: parentDomain,
      });

      setIsAuthenticated(true);
      setHandle(handle);

      // Set cookies required for the Habitat chrome extensioon
      if (source === "chrome_extension") {
        Cookies.set("chrome_extension_user_id", did);
        Cookies.set("chrome_extension_access_token", accessJwt);
        Cookies.set("chrome_extension_refresh_token", refreshJwt);
      }

      if (!redirectRoute) {
        redirectRoute = "/";
      }

      router.navigate({ to: redirectRoute });
    } catch (err) {
      throw new Error("Login failed");
    }
  };

  const logout = () => {
    Cookies.remove("access_token");
    Cookies.remove("refresh_token");
    Cookies.remove("chrome_extension_user_id");
    Cookies.remove("chrome_extension_access_token");
    Cookies.remove("chrome_extension_refresh_token");
    Cookies.remove("handle");

    // Oauth sessions
    Cookies.remove("auth-session");
    Cookies.remove("dpop-session");

    setIsAuthenticated(false);
    router.navigate({ to: "/login" });
  };

  return (
    <AuthContext.Provider value={{ isAuthenticated, handle, login, logout }}>
      {children}
    </AuthContext.Provider>
  );
};

export const useAuth = (): AuthContextType => {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
};
