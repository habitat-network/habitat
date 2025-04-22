'use client'
import React, { useState, FormEvent } from 'react';
import axios from 'axios';
import Cookies from 'js-cookie';
import './addUser.css';

const Register: React.FC = () => {
    const [email, setEmail] = useState<string>('');
    const [handle, setHandle] = useState<string>('');
    const [password, setPassword] = useState<string>('');
    const [confirmPassword, setConfirmPassword] = useState<string>('');
    const [error, setError] = useState<string>('');

    const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
        event.preventDefault();

        if (password !== confirmPassword) {
            setError('Passwords do not match');
            return;
        }

        try {
            // TODO: this should just pass through to the PDS xrpc endpoint -- unfortunately there's a bit more node setup that happens.
            const response = await fetch(`${window.location.origin}/habitat/api/node/users`, {
                method: 'POST',
                body: JSON.stringify({
                    email,
                    handle,
                    password
                }),
                headers: {
                    'Content-Type': 'application/json',
                }
            })

            const { access_token } = await response.json();

            // Set the access token in a cookie
            Cookies.set('access_token', access_token, { expires: 7 });

            console.log('Registration successful, token set in cookie');
        } catch (error) {
            setError('Registration failed');
            console.error('Registration error', error);
        }
    };

    return (
        <div className="register-container">
            <form className="register-form" onSubmit={handleSubmit}>
                <h2>Register</h2>
                {error && <p className="error">{error}</p>}
                <div className="form-group">
                    <label htmlFor="email">Email:</label>
                    <input
                        type="email"
                        id="email"
                        value={email}
                        onChange={(e) => setEmail(e.target.value)}
                        required
                    />
                </div>
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
                <div className="form-group">
                    <label htmlFor="confirm-password">Confirm Password:</label>
                    <input
                        type="password"
                        id="confirm-password"
                        value={confirmPassword}
                        onChange={(e) => setConfirmPassword(e.target.value)}
                        required
                    />
                </div>
                <button type="submit">Register</button>
            </form>
        </div>
    );
};

export default Register;
