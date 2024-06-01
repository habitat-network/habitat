# Use an official Node runtime as a parent image
FROM node:20-slim 

# Set the working directory in the container
WORKDIR /app

# Install pnpm
RUN npm install -g pnpm

# Copy package.json and other related files
COPY ./frontend/package.json ./frontend/pnpm-lock.yaml ./

# Install dependencies
RUN pnpm install

# Copy the rest of your application's code
COPY ./frontend/ .

# Your app binds to port 3000, make sure the container does too
EXPOSE 8000

# Command to run your app using pnpm
CMD [ "pnpm", "dev"]
