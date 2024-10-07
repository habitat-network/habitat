'use client'

import React, { useState, FormEvent, Suspense } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import './login.css';
import { useAuth } from '@/components/authContext';

const LoginInternal = () => {
    const [handle, setHandle] = useState('');
    const [password, setPassword] = useState('');
    const { login } = useAuth();

    useRouter();

    const searchParams = useSearchParams();

    const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
        let redirectRoute: string = '/home';
        const overrideRoute =  searchParams.get('redirectRoute');
        if (overrideRoute) {
            redirectRoute = overrideRoute;
        }

        const source = searchParams.get('source');

        event.preventDefault();
        login(handle, password, redirectRoute, source);
    };

    return (
        <div className="login-container">
            <form className="login-form" onSubmit={handleSubmit}>
                <h2>Login</h2>
                <div className="form-group">
                    <label htmlFor="handle">Handle:</label>
                    <input
                        type="text"
                        id="handle"
                        value={handle}
                        onChange={(e) => setHandle(e.target.value)}
                        required
                    />
                </div>
                <div className="form-group">
                    <label htmlFor="password">Password:</label>
                    <input
                        type="password"
                        id="password"
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        required
                    />
                </div>
                <button type="submit">Login</button>
            </form>
        </div>
    );
};

const Login = () => {
    return (
        <Suspense fallback={<div>Loading...</div>}>
            <LoginInternal />
        </Suspense>
    );
};

export default Login;