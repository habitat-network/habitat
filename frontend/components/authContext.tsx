'use client'

import React, { createContext, useContext, useState, ReactNode, useEffect } from 'react';
import Cookies from 'js-cookie';
import axios from 'axios';
import { useRouter } from 'next/navigation';

interface AuthContextType {
    isAuthenticated: boolean;
    login: (email: string, password: string) => Promise<void>;
    logout: () => void;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
    const token = Cookies.get('access_token');
    const authed = token ? true : false;
    const [isAuthenticated, setIsAuthenticated] = useState<boolean>(authed);
    const router = useRouter();

    const login = async (identifier: string, password: string) => {
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

            const { accessJwt, refreshJwt } = response.data;


            // Set the access token in a cookie
            Cookies.set('access_token', accessJwt, { expires: 7 });
            Cookies.set('refresh_token', refreshJwt, { expires: 7 });
            setIsAuthenticated(true);
            console.log("pushhh")

            router.push('/home');

        } catch (err) {
            console.error(err);
            throw new Error('Login failed');
        }
    };

    const logout = () => {
        Cookies.remove('access_token');
        Cookies.remove('refresh_token');

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