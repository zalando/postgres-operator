const DEBUG = process.env.NODE_ENV !== 'production'
const entry = ['./src/app.js']
const path = require('path')
const pkg = require('./package.json')
const webpack = require('webpack')

module.exports = {
    context: path.join(__dirname, './'),
    devtool: DEBUG ? 'inline-source-map' : false,
    entry: entry,
    mode: DEBUG ? 'development' : 'production',
    target: 'web',

    node: {
        fs: 'empty'
    },

    externals: {
        '$': '$',
        'jquery': 'jQuery',
    },

    output: {
    // filename: DEBUG ? 'app.js' : 'app-[hash].js'
        filename: 'app.js',
        library: 'App',
        path: path.resolve(pkg.config.buildDir),
        publicPath: DEBUG ? '/' : './',
    },

    plugins: [
        new webpack.optimize.OccurrenceOrderPlugin(),
        new webpack.HotModuleReplacementPlugin(),

        new webpack.LoaderOptionsPlugin({
            debug: DEBUG,
        }),

        // Print on rebuild when watching; see
        // https://github.com/webpack/webpack/issues/1499#issuecomment-155064216
        function () {
            this.plugin('watch-run', (watching, callback) => {
                console.log('Begin compile at ' + new Date())
                callback()
            })
        },

    ],

    module: {

        rules: [

            {
                test: /\.tag\.pug$/,
                loader: 'riot-tag-loader',
                exclude: /node_modules/,
                query: {
                    hot: true,
                    template: 'pug',
                    type: 'es6',
                },
            },

            {
                test: /\.tag$/,
                loader: 'riot-tag-loader',
                exclude: /node_modules/,
                query: {
                    hot: false,
                    type: 'es6',
                },
            },

            {
                test: /\.js$/,
                loader: 'babel-loader',
                exclude: /node_modules/,
                query: {
                    plugins: ['@babel/transform-runtime'],
                    presets: ['@babel/preset-env'],
                },
            },

            {
                test: /\.html$/,
                loader: 'file-loader?name=[path][name].[ext]',
                exclude: /node_modules/,
            },

            {
                test: /\.jpe?g$|\.svg$|\.png$/,
                loader: 'file-loader?name=[path][name].[ext]',
                exclude: /node_modules/,
            },

            {
                test: /\.json$/,
                loader: 'json',
                exclude: /node_modules/,
            },

            {
                test: /\.(otf|eot|svg|ttf|woff|woff2)(\?v=\d+\.\d+\.\d+)?$/,
                loader: 'url?limit=8192&mimetype=application/font-woff',
            },

            {
                test: /\.json$/,
                loader: 'json',
                include: path.join(__dirname, 'node_modules', 'pixi.js'),
            },

        ],

    },

}
