# Use an official Node runtime as a parent image
FROM node:20-slim 

# Set the working directory in the container
WORKDIR /app

ENV PATH ./node_modules/.bin:$PATH

# Install pnpm
RUN npm install -g pnpm

# Copy package.json and other related files
COPY ./frontend/package.json ./pnpm-lock.yaml ./

# Install dependencies
RUN pnpm install

# Copy the rest of your application's code
COPY ./frontend/ .

# Your app binds to port 5173, make sure the container does too
EXPOSE 5173

# Command to run your app using pnpm
CMD ["pnpm", "start"]
