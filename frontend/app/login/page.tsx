'use client'

import React, { useState, FormEvent, Suspense } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import styles from './login.module.css';
import { useAuth } from '@/components/authContext';

const LoginInternal = () => {
    const [handle, setHandle] = useState('');
    const [password, setPassword] = useState('');
    const { login } = useAuth();

    useRouter();

    const searchParams = useSearchParams();

    const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
        let redirectRoute: string = '/';
        const overrideRoute =  searchParams.get('redirectRoute');
        if (overrideRoute) {
            redirectRoute = overrideRoute;
        }

        const source = searchParams.get('source');

        event.preventDefault();
        login(handle, password, redirectRoute, source);
    };

    return (
        <div className={styles.loginBody}>
            <div className={styles.loginContainer}>
                <form className={styles.loginForm} onSubmit={handleSubmit}>
                <h2>Login</h2>
                <div className={styles.formGroup}>
                    <label htmlFor="handle">Handle:</label>
                    <input
                        type="text"
                        id="handle"
                        value={handle}
                        onChange={(e) => setHandle(e.target.value)}
                        required
                    />
                </div>
                <div className={styles.formGroup}>
                    <label htmlFor="password">Password:</label>
                    <input
                        type="password"
                        id="password"
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        required
                    />
                </div>
                <button className={styles.loginButton} type="submit">Login</button>
                </form>
            </div>
        </div>
    );
};

const Login = () => {
    // TODO redirect to home if already logged in
    return (
        <Suspense fallback={<div>Loading...</div>}>
            <LoginInternal />
        </Suspense>
    );
};

export default Login;