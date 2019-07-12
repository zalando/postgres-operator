This directory contains the EcmaScript frontend code of the PostgreSQL Operator UI and is only needed during build time.

The JavaScript application bundle (webpack) will be generated to ``operator_ui/static/build/app*.js`` by running:

.. code-block:: bash

    $ npm install
    $ npm run build

Frontend development is supported by watching the source code and continuously recompiling the webpack:

.. code-block:: bash

    $ npm start
