'use client'

import React, { createContext, useContext, useState, ReactNode, useEffect } from 'react';
import Cookies from 'js-cookie';
import axios from 'axios';
import { useRouter } from 'next/navigation';

interface AuthContextType {
    isAuthenticated: boolean;
    login: (email: string, password: string, redirectRoute: string | null, source: string | null) => Promise<void>;
    logout: () => void;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
    const token = Cookies.get('access_token');
    const authed = token ? true : false;
    const [isAuthenticated, setIsAuthenticated] = useState<boolean>(authed);
    const router = useRouter();

    const login = async (
        identifier: string,
        password: string,
        redirectRoute: string | null = null,
        source: string | null = null
    ) => {
        try {
            const response = await axios.post(`${window.location.origin}/habitat/api/node/login`, {
                password: password,
                identifier: identifier,
              }, {
                headers: {
                  'Content-Type': 'application/json',
                },
            });
            console.log(response.data);

            const { accessJwt, refreshJwt, did } = response.data;

            // If we are using a *ts.net domain, make sure the cookies are applied to all other subdomains on that TailNet.
            let parentDomain = window.location.hostname;
            if (window.location.hostname.endsWith(".ts.net")) {
                const parts = window.location.hostname.split(".")
                if (parts.length > 2) {
                    parentDomain = parts.slice(-3).join(".");
                }
            }

            // Set the access token in a cookie
            Cookies.set('access_token', accessJwt, {
                expires: 7,
                domain: parentDomain,
            });
            Cookies.set('refresh_token', refreshJwt, {
                expires: 7,
                domain: parentDomain,
            });
            // To help dev app frontends figure out where to make API requests.
            Cookies.set('habitat_domain', window.location.hostname, {
                expires: 7,
                domain: parentDomain,
            });

            // The user's did
            Cookies.set('user_did', did, {
                expires: 7,
                domain: parentDomain,
            });

            setIsAuthenticated(true);

            // Set cookies required for the Habitat chrome extensioon
            if (source === 'chrome_extension') {
                Cookies.set('chrome_extension_user_id', did);
                Cookies.set('chrome_extension_access_token', accessJwt);
                Cookies.set('chrome_extension_refresh_token', refreshJwt);
            }

            if (!redirectRoute) {
                redirectRoute = '/home';
            }
            router.push(redirectRoute);

        } catch (err) {
            throw new Error('Login failed');
        }
    };

    const logout = () => {
        Cookies.remove('access_token');
        Cookies.remove('refresh_token');

        Cookies.remove('chrome_extension_user_id');
        Cookies.remove('chrome_extension_access_token');
        Cookies.remove('chrome_extension_refresh_token');

        setIsAuthenticated(false);
        router.push('/login');
    };

    return (
        <AuthContext.Provider value={{ isAuthenticated, login, logout }}>
            {children}
        </AuthContext.Provider>
    );
};

export const useAuth = (): AuthContextType => {
    const context = useContext(AuthContext);
    if (context === undefined) {
        throw new Error('useAuth must be used within an AuthProvider');
    }
    return context;
};