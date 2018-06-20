Sentiment Service
=================

Provides a simple wrapper over the Google sentiment analysis API.


Building
--------

The easiest way to build the project is to use Docker. Invoke the following command to build the Docker image:

```
make docker
```

Running
-------

- In order to run the service, a valid Google Cloud service account key is required. Follow the instructions at https://cloud.google.com/docs/authentication/getting-started to create and download the key.
- Rename the downloaded service account key file to `service_account.json`
- Launch the Docker image using the following command:

```
docker run -it -v $(pwd):/credentials -p 8080:8080 charithe/sentiment
```

The above command will launch the service on port 8080. In order to test the service, issue a request as follows:

```
curl -XPOST 'localhost:8080/api?order=desc' -d '{"content": "I hate this site. But I love the product"}'
```


To Do
-----

- More compact caching of results using a custom representation 
- Find a better, space-efficient cache key instead of using the full document in its' entirety
- Add metrics
- Add circuit-breaking and rate-limiting
- Add support for HTTPS
- Create Kubernetes deployment charts


