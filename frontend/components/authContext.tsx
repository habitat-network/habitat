'use client'

import React, { createContext, useContext, useState, ReactNode, useEffect } from 'react';
import Cookies from 'js-cookie';
import axios from 'axios';
import { useRouter } from 'next/navigation';
import Header from './header';

interface AuthContextType {
    isAuthenticated: boolean;
    handle: string | null;
    login: (email: string, password: string, redirectRoute: string | null, source: string | null) => Promise<void>;
    logout: () => void;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider: React.FC<{ children: ReactNode }> = ({ children }) => {


    const [isAuthenticated, setIsAuthenticated] = useState<boolean>(true);

    const [handle, setHandle] = useState<string | null>(null);
    const router = useRouter();

    useEffect(() => {
        const token = Cookies.get('access_token');
        const authed = token ? true : false;
        setIsAuthenticated(authed);
    }, []);

    useEffect(() => {
        const handle = Cookies.get('handle');
        if (handle) {
            setHandle(handle);
        }
    }, []);


    const login = async (
        identifier: string,
        password: string,
        redirectRoute: string | null = null,
        source: string | null = null
    ) => {
        try {
            // If we are using a *ts.net domain, make sure the cookies are applied to all other subdomains on that TailNet.
            let parentDomain = window.location.hostname;
            if (window.location.hostname.endsWith(".ts.net")) {
                const parts = window.location.hostname.split(".")
                if (parts.length > 2) {
                    parentDomain = parts.slice(-3).join(".");
                }
            }

            const fullHandle = parentDomain == "localhost" ? `${identifier}` : `${identifier}.${window.location.hostname}`;
            const response = await axios.post(`${window.location.origin}/habitat/api/node/login`, {
                password: password,
                identifier: fullHandle,
            }, {
                headers: {
                    'Content-Type': 'application/json',
                },
            });
            console.log(response.data);

            const { accessJwt, refreshJwt, did, handle } = response.data;


            // Set the access token in a cookie
            Cookies.set('access_token', accessJwt, {
                expires: 7,
                ...(parentDomain != ".localhost" && { domain: parentDomain }),
            });
            Cookies.set('refresh_token', refreshJwt, {
                expires: 7,
                ...(parentDomain != ".localhost" && { domain: parentDomain }),
            });
            // The user's did
            Cookies.set('user_did', did, {
                expires: 7,
                ...(parentDomain != ".localhost" && { domain: parentDomain }),
            });

            Cookies.set('handle', handle, {
                expires: 7,
                ...(parentDomain != ".localhost" && { domain: parentDomain }),
            });
            // To help dev app frontends figure out where to make API requests.
            Cookies.set('habitat_domain', window.location.hostname, {
                expires: 7,
                domain: parentDomain,
            });

            setIsAuthenticated(true);
            setHandle(handle);

            // Set cookies required for the Habitat chrome extensioon
            if (source === 'chrome_extension') {
                Cookies.set('chrome_extension_user_id', did);
                Cookies.set('chrome_extension_access_token', accessJwt);
                Cookies.set('chrome_extension_refresh_token', refreshJwt);
            }

            if (!redirectRoute) {
                redirectRoute = '/';
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
        Cookies.remove('handle');

        setIsAuthenticated(false);
        router.push('/login');
    };

    return (
        <AuthContext.Provider value={{ isAuthenticated, handle, login, logout }}>
            <div className="flex flex-col items-center justify-center w-full h-screen">
                <div className="flex flex-col items-center justify-center w-full">
                    <Header isAuthenticated={isAuthenticated} handle={handle} logout={logout} />
                </div>
                <div className="flex flex-col items-center justify-center w-full h-screen">
                    {children}
                </div>
            </div>
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