'use client'

import React, { useEffect } from 'react';
import { useAuth } from './authContext';
import { useRouter } from '@tanstack/react-router';

const withAuth = (WrappedComponent: React.FC) => {
    const ComponentWithAuth = (props: any) => {
        const { isAuthenticated } = useAuth();
        const router = useRouter();

        useEffect(() => {
            if (!isAuthenticated) {
                router.navigate({ to: '/login' });
            }
        }, [isAuthenticated, router]);

        return <WrappedComponent {...props} />;
    };

    return ComponentWithAuth;
};

export default withAuth;
